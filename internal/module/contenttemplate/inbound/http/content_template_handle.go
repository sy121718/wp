// content_template_handle.go — 内容模板列表与编辑入口（EDT-001）。
//
// 模板 Document 的可视化编辑走 /workbench?template=…（复用页面工作台）；
// 本文件提供后台列表页与带样例实体参数的编辑跳转。
package contenttemplatehttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

const contentTemplatesListPath = "/admin/content-templates"

// contentTemplatesNotReadyText 能力未装配时的回执（本页在 h.templates 为空时整体降级）。
const contentTemplatesNotReadyText = "内容模板能力未装配，无法删除。"

// contentTemplatePageHandle 内容模板后台页。
type contentTemplatePageHandle struct {
	templates contenttemplatecontract.ContentTemplateService
	projects  projectcontract.ProjectService
	products  productcontract.ProductService
	contents  contentcontract.ContentService
}

func newContentTemplatePageHandle(templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) *contentTemplatePageHandle {
	return &contentTemplatePageHandle{
		templates: templates, projects: projects, products: products, contents: contents,
	}
}

// ContentTemplatesPage GET /admin/content-templates：按实体类型列出模板，跳转可视化编辑。
func (h *contentTemplatePageHandle) ContentTemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	data := gin.H{
		"title": "MsgContentTemplatesTitle", "menu": "content-templates",
		"Projects": projects, "SelectedProject": selected,
		"EntityType": entityType, "Err": strings.TrimSpace(c.Query("err")),
	}
	if h.templates == nil {
		data["Ready"] = false
		c.HTML(http.StatusOK, "admin/content_templates.html", shell.Prepare(c, data))
		return
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: entityType})
	if err != nil {
		shell.PageError(c, "content_template", err)
		return
	}
	rows := make([]gin.H, 0, len(list))
	for _, t := range list {
		sampleID, sampleErr := h.sampleEntityID(ctx, selected, t.EntityType)
		editURL := ""
		if sampleErr == nil && sampleID != "" {
			editURL = workbenchTemplateURL(t.ID, t.EntityType, sampleID, selected)
		}
		rows = append(rows, gin.H{
			"ID": t.ID, "Name": t.Name, "EntityType": t.EntityType,
			"DraftVersion": t.DraftVersion, "UpdatedAt": t.UpdatedAt,
			"EditURL": editURL, "SampleErr": sampleErrMsg(sampleErr, t.EntityType),
		})
	}
	data["Templates"] = rows
	data["TemplateCount"] = len(rows)
	data["Ready"] = true
	// 可选键一律由 handler 注入（模板用 isset 包裹）：本页此前只有 ?err=，
	// 批量删除的「成功 N 个 / 跳过 M 个」需要一条正向回执通道。
	data["Done"] = strings.TrimSpace(c.Query("done"))
	c.HTML(http.StatusOK, "admin/content_templates.html", shell.Prepare(c, data))
}

// ContentTemplatesBulkDelete 批量删除内容模板（POST /admin/content-templates/bulk-delete）。
//
// 逐条走**同一条单条删除路径**（h.templates.Delete）：被自动发布实例引用的模板由数据库
// 外键拒绝，其余照常删除 —— 单条失败不中断整批（整批回滚会让用户以为「一个都没删」然后反复重试）。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，并保留工程与实体类型筛选。
func (h *contentTemplatePageHandle) ContentTemplatesBulkDelete(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	entityType := strings.TrimSpace(c.PostForm("entityType"))
	q := url.Values{}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if entityType != "" {
		q.Set("entityType", entityType)
	}
	if h.templates == nil {
		q.Set("err", contentTemplatesNotReadyText)
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?"+q.Encode())
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		q.Set("err", berr.Error())
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?"+q.Encode())
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.templates.Delete(c.Request.Context(), &contenttemplatedto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	if msg := contentTemplatesBulkDeleteResult(deleted, skipped); msg != "" {
		if skipped > 0 {
			q.Set("err", msg)
		} else {
			q.Set("done", msg)
		}
	}
	c.Redirect(http.StatusFound, contentTemplatesListPath+"?"+q.Encode())
}

// contentTemplatesBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」等于把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func contentTemplatesBulkDeleteResult(deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return "没有选中任何模板，列表未改动。"
	case skipped == 0:
		return fmt.Sprintf("已删除 %d 个模板。", deleted)
	case deleted == 0:
		return fmt.Sprintf("%d 个模板都未能删除，列表未改动（被自动发布实例引用的模板不能删除）。", skipped)
	default:
		return fmt.Sprintf("已删除 %d 个，%d 个未能删除（被自动发布实例引用的模板不能删除）。", deleted, skipped)
	}
}

// ContentTemplateEditPage GET /admin/content-templates/edit：302 到工作台（保留 query）。
func (h *contentTemplatePageHandle) ContentTemplateEditPage(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("id"))
	if templateID == "" {
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?err=缺少模板 id")
		return
	}
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	if entityID == "" && h.templates != nil {
		tpl, err := h.templates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
		if err != nil {
			c.Redirect(http.StatusFound, contentTemplatesListPath+"?err=模板不存在")
			return
		}
		if entityType == "" {
			entityType = tpl.EntityType
		}
		if projectID == "" {
			projectID = strings.TrimSpace(c.Query("project"))
		}
		sampleID, serr := h.sampleEntityID(c.Request.Context(), projectID, entityType)
		if serr != nil {
			c.Redirect(http.StatusFound, contentTemplatesListPath+"?project="+projectID+"&err="+url.QueryEscape(serr.Error()))
			return
		}
		entityID = sampleID
	}
	if entityID == "" || entityType == "" {
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?err=缺少预览样例实体")
		return
	}
	c.Redirect(http.StatusFound, workbenchTemplateURL(templateID, entityType, entityID, projectID))
}

func workbenchTemplateURL(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return "/workbench?" + q.Encode()
}

func (h *contentTemplatePageHandle) sampleEntityID(ctx context.Context, projectID, entityType string) (string, error) {
	switch entityType {
	case "product":
		if h.products == nil {
			return "", fmt.Errorf("商品模块未装配")
		}
		list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Page: 1, Size: 1})
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "", fmt.Errorf("工程内还没有商品，无法预览商品详情模板")
		}
		return list[0].ID, nil
	case "article":
		if h.contents == nil {
			return "", fmt.Errorf("内容模块未装配")
		}
		list, err := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: 1})
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "", fmt.Errorf("工程内还没有文章，无法预览文章详情模板")
		}
		return list[0].ID, nil
	default:
		return "", fmt.Errorf("暂不支持 entityType=%s 的样例实体自动选取", entityType)
	}
}

func sampleErrMsg(err error, entityType string) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
