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

// ContentTemplatePageHandle 导出类型别名：外部测试包（public/test/contenttemplate/feature）
// 需要命名构造器返回的句柄类型才能驱动页面处理器。
// 别名指向未导出类型是合法 Go，读起来也明确指向后者（对齐 page 模块的 PagesAdminHandle）。
type ContentTemplatePageHandle = contentTemplatePageHandle

// NewContentTemplatePageHandle 构造内容模板后台页处理器（装配与测试共用同一入口）。
func NewContentTemplatePageHandle(templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) *ContentTemplatePageHandle {
	return newContentTemplatePageHandle(templates, projects, products, contents)
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
		"EntityType": entityType,
		// 回执文案经本页白名单收口（查询参数不是可信边界，见 content_template_err.go）。
		"Err": contentTemplatePageErr(c),
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
		sampleID, sampleHint, sampleErr := h.sampleEntityID(ctx, selected, t.EntityType)
		editURL := ""
		if sampleErr == nil && strings.TrimSpace(sampleHint) == "" && sampleID != "" {
			editURL = workbenchTemplateURL(t.ID, t.EntityType, sampleID, selected)
		}
		rows = append(rows, gin.H{
			"ID": t.ID, "Name": t.Name, "EntityType": t.EntityType,
			"DraftVersion": t.DraftVersion, "UpdatedAt": t.UpdatedAt,
			// SampleErr 是**模板数据**（形态③）：依赖错误只能出归口文案，原文进日志。
			"EditURL": editURL, "SampleErr": sampleErrText(c, sampleHint, sampleErr),
		})
	}
	data["Templates"] = rows
	data["TemplateCount"] = len(rows)
	data["Ready"] = true
	// 可选键一律由 handler 注入（模板用 isset 包裹）：本页此前只有 ?err=，
	// 批量删除的「成功 N 个 / 跳过 M 个」需要一条正向回执通道。
	data["Done"] = contentTemplatePageDone(c)
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
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		q.Set("err", contentTemplateErrText(c, berr))
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
	// 模板取自 content_template_err.go 的 contentTemplatesBulkResultTemplates ——
	// 那里同时是读侧白名单的来源：写侧改措辞时读侧跟着变，不会静默失配成归口文案。
	switch {
	case deleted == 0 && skipped == 0:
		return contentTemplatesBulkResultTemplates[0]
	case skipped == 0:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[1], deleted)
	case deleted == 0:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[2], skipped)
	default:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[3], deleted, skipped)
	}
}

// ContentTemplateEditPage GET /admin/content-templates/edit：302 到工作台（保留 query）。
func (h *contentTemplatePageHandle) ContentTemplateEditPage(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("id"))
	if templateID == "" {
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?err="+url.QueryEscape(contentTemplateMissingIDText))
		return
	}
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	if entityID == "" && h.templates != nil {
		tpl, err := h.templates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
		if err != nil {
			c.Redirect(http.StatusFound, contentTemplatesListPath+"?err="+url.QueryEscape(contentTemplateNotFoundText))
			return
		}
		if entityType == "" {
			entityType = tpl.EntityType
		}
		if projectID == "" {
			projectID = strings.TrimSpace(c.Query("project"))
		}
		sampleID, hint, serr := h.sampleEntityID(c.Request.Context(), projectID, entityType)
		if serr != nil || strings.TrimSpace(hint) != "" {
			c.Redirect(http.StatusFound, contentTemplatesListPath+"?project="+url.QueryEscape(projectID)+
				"&err="+url.QueryEscape(sampleErrText(c, hint, serr)))
			return
		}
		entityID = sampleID
	}
	if entityID == "" || entityType == "" {
		c.Redirect(http.StatusFound, contentTemplatesListPath+"?err="+url.QueryEscape(contentTemplateSampleMissingText))
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

// sampleEntityID 为某实体类型选一条预览样例。
//
// 两个返回值分开是**故意的**：hint 是可直接展示的可行动提示（依赖没装配 / 工程内还没有
// 可预览的实体 / 该实体类型不支持），err 是依赖错误（products.List / contents.List 上抛，
// 可能是 PG 原文，带表名与 SQLSTATE）。合成一个 error 就再也分不清「该给运营看」还是
// 「只该进日志」—— 这一页的 ?err= 与 SampleErr 都会被原样渲染。
func (h *contentTemplatePageHandle) sampleEntityID(ctx context.Context, projectID, entityType string) (id, hint string, err error) {
	switch entityType {
	case "product":
		if h.products == nil {
			return "", contentTemplateHintProductNoMod, nil
		}
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Page: 1, Size: 1})
		if lerr != nil {
			return "", "", lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoProduct, nil
		}
		return list[0].ID, "", nil
	case "article":
		if h.contents == nil {
			return "", contentTemplateHintContentNoMod, nil
		}
		list, lerr := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: 1})
		if lerr != nil {
			return "", "", lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoArticle, nil
		}
		return list[0].ID, "", nil
	default:
		// 实体类型来自查询参数：只回一句**固定**文案，不回显入参 ——
		// 回显等于把请求方的输入原样反射进 ?err= / SampleErr 再渲染一次。
		return "", contentTemplateHintEntityTypeMiss, nil
	}
}
