// content_template_handle.go — 内容模板列表与编辑入口（EDT-001）。
//
// 模板 Document 的可视化编辑走 /workbench?template=…（复用页面工作台）；
// 本文件提供后台列表页与带样例实体参数的编辑跳转。
package dashboardhttp

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
	dashboardenums "go_wp/internal/module/dashboard/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

const contentTemplatesListPath = "/admin/content-templates"

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
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
		c.HTML(http.StatusOK, "admin/content_templates.html", withCSRF(c, data))
		return
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: entityType})
	if err != nil {
		pageError(c, "content_template", err)
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
	c.HTML(http.StatusOK, "admin/content_templates.html", withCSRF(c, data))
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
