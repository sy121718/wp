package contenttemplatehttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/builder"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const contentTemplatesListPath = "/admin/content-templates"

const contentTemplatesNotReadyText = "内容模板能力未装配，无法删除。"

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

type ContentTemplatePageHandle = contentTemplatePageHandle

func NewContentTemplatePageHandle(templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) *ContentTemplatePageHandle {
	return newContentTemplatePageHandle(templates, projects, products, contents)
}

func (h *contentTemplatePageHandle) ContentTemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {

		pageview.FailScene(c, contentTemplateErrScene, err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	view := &contenttemplatedto.ContentTemplatesPageResp{
		Title: "MsgContentTemplatesTitle", Menu: "content-templates",
		Projects: projects, SelectedProject: selected,
		EntityType: entityType,

		Err:   facing.Msg{},
		Ready: true,
	}
	if h.templates == nil {

		view.Ready = false
		pageview.Render(c, view)
		return
	}

	impact, impactErr := h.templates.Impact(ctx, &contenttemplatedto.ImpactReq{ProjectID: selected})
	if impactErr != nil || impact == nil {
		impact = &contenttemplatedto.ImpactResp{Available: false}
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: entityType, ProjectID: selected})
	if err != nil {

		pageview.FailScene(c, contentTemplateErrScene, err)
		return
	}

	view.Templates = h.templateRows(c, list, contentTemplateRefsByTemplate(c, impact), selected)
	view.TemplateCount = len(view.Templates)
	view.Ready = true

	view.ImpactAvailable = impact.Available
	view.ImpactNote = contentTemplateImpactNote(impact, impactErr)

	pageview.Render(c, view)
}

// templateRows 把模板实体与引用面折成列表行：逐行映射归控制器，
// 但取词只交事实（LabelPair / Msg），成品句由模板按请求语言生成。
func (h *contentTemplatePageHandle) templateRows(c *gin.Context,
	list []*contenttemplatedto.TemplateResp,
	refsByTemplate map[string]*contentTemplateImpactRow, projectID string) []contenttemplatedto.TemplateListRowResp {
	rows := make([]contenttemplatedto.TemplateListRowResp, 0, len(list))
	for _, t := range list {
		isStructure := contenttemplatemodel.IsStructureTemplateType(t.EntityType)
		editURL := ""
		var sampleNote facing.Msg
		if isStructure {
			editURL = workbenchTemplateURL(t.ID, t.EntityType, "", projectID)
		} else {
			sampleID, sampleHint, sampleErr := h.sampleEntityID(c.Request.Context(), projectID, t.EntityType)
			if sampleErr == nil && strings.TrimSpace(sampleHint.Key) == "" && sampleID != "" {
				editURL = workbenchTemplateURL(t.ID, t.EntityType, sampleID, projectID)
			}
			sampleNote = sampleErrMsg(c, sampleHint, sampleErr)
		}
		refs := refsByTemplate[t.ID]
		if refs == nil {
			refs = &contentTemplateImpactRow{Pages: []contentTemplatePageRef{}, Instances: []contentTemplateInstanceRef{}}
		}
		rows = append(rows, contenttemplatedto.TemplateListRowResp{
			ID: t.ID, Name: t.Name, EntityType: t.EntityType,
			DraftVersion: t.DraftVersion, UpdatedAt: t.UpdatedAt,

			IsStructure: isStructure,
			Type:        contenttemplateenums.EntityTypeLabel(t.EntityType),
			Role:        contenttemplateenums.TemplateRoleLabel(t.TemplateRole),
			IsDefault:   t.IsDefault,

			EditURL: editURL, SampleNote: sampleNote,

			RefPages:         toTemplatePageRefs(refs.Pages),
			RefInstances:     toTemplateInstanceRefs(refs.Instances),
			RefCount:         refs.Count,
			RefPageCount:     len(refs.Pages),
			RefInstanceCount: len(refs.Instances),
		})
	}
	return rows
}

func (h *contentTemplatePageHandle) ContentTemplateEditPage(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("id"))
	if templateID == "" {
		contentTemplateJump(c, false, contentTemplateMissingIDMsg)
		return
	}
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	if projectID == "" {
		projectID = strings.TrimSpace(c.Query("project"))
	}
	if h.templates != nil {
		tpl, err := h.templates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
		if err != nil {
			contentTemplateJump(c, false, contentTemplateNotFoundMsg)
			return
		}
		if contenttemplatemodel.IsStructureTemplateType(tpl.EntityType) {
			c.Redirect(http.StatusFound, workbenchTemplateURL(templateID, tpl.EntityType, "", projectID))
			return
		}
		if entityType == "" {
			entityType = tpl.EntityType
		}
		if entityID == "" {
			sampleID, hint, serr := h.sampleEntityID(c.Request.Context(), projectID, entityType)
			if serr != nil || strings.TrimSpace(hint.Key) != "" {
				contentTemplateJump(c, false, sampleErrMsg(c, hint, serr))
				return
			}
			entityID = sampleID
		}
	}
	if entityID == "" || entityType == "" {
		contentTemplateJump(c, false, contentTemplateSampleMissingMsg)
		return
	}
	c.Redirect(http.StatusFound, workbenchTemplateURL(templateID, entityType, entityID, projectID))
}

func workbenchTemplateURL(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	if entityType != "" {
		q.Set("entityType", entityType)
	}
	if entityID != "" {
		q.Set("entityId", entityID)
	}
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return "/workbench?" + q.Encode()
}

// sampleEntityID 返回可用于预览的样例实体；取不到时给出**原因事实**（key + 兜底），
// 不在这里拼成品句 —— 页面与回跳提示都由各自的取词出口处理。
func (h *contentTemplatePageHandle) sampleEntityID(ctx context.Context, projectID, entityType string) (id string, hint facing.Msg, err error) {
	switch entityType {
	case "product":
		if h.products == nil {
			return "", contentTemplateHintProductNoMod, nil
		}
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Page: 1, Size: 1})
		if lerr != nil {
			return "", facing.Msg{}, lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoProduct, nil
		}
		return list[0].ID, facing.Msg{}, nil
	case "article":
		if h.contents == nil {
			return "", contentTemplateHintContentNoMod, nil
		}
		list, lerr := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: 1})
		if lerr != nil {
			return "", facing.Msg{}, lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoArticle, nil
		}
		return list[0].ID, facing.Msg{}, nil
	default:
		return "", contentTemplateHintEntityTypeMiss, nil
	}
}

func contentTemplateLabel(c *gin.Context, pair contenttemplateenums.LabelPair) string {
	if strings.TrimSpace(pair.Key) == "" {
		return pair.Fallback
	}
	return response.MsgText(c, facing.Msg{Key: pair.Key, Fallback: pair.Fallback})
}

type contentTemplatePageRef struct {
	Label string
	Path  string
	Slots []contenttemplateenums.LabelPair
	URL   string
}

type contentTemplateInstanceRef struct {
	Label  string
	Entity string
	Path   string
	Slots  []contenttemplateenums.LabelPair
}

type contentTemplateImpactRow struct {
	Pages     []contentTemplatePageRef
	Instances []contentTemplateInstanceRef
	Count     int
}

func contentTemplateRefsByTemplate(c *gin.Context, impact *contenttemplatedto.ImpactResp) map[string]*contentTemplateImpactRow {
	out := map[string]*contentTemplateImpactRow{}
	if impact == nil {
		return out
	}
	for _, ref := range impact.References {
		row := out[ref.TemplateID]
		if row == nil {
			row = &contentTemplateImpactRow{Pages: []contentTemplatePageRef{}, Instances: []contentTemplateInstanceRef{}}
			out[ref.TemplateID] = row
		}
		slots := contentTemplateSlotsLabel(ref.Slots)
		switch ref.Kind {
		case contenttemplatedto.ReferenceKindPage:
			label := strings.TrimSpace(ref.PageTitle)
			if label == "" {
				label = strings.TrimSpace(ref.PagePath)
			}
			if label == "" {
				label = ref.PageID
			}
			row.Pages = append(row.Pages, contentTemplatePageRef{
				Label: label, Path: strings.TrimSpace(ref.PagePath), Slots: slots,

				URL: "/workbench?id=" + url.QueryEscape(ref.PageID),
			})
		case contenttemplatedto.ReferenceKindInstance:
			label := strings.TrimSpace(ref.URLPath)
			if label == "" {
				label = strings.TrimSpace(ref.EntityType + " " + ref.EntityID)
			}
			row.Instances = append(row.Instances, contentTemplateInstanceRef{
				Label: label, Entity: strings.TrimSpace(ref.EntityType + " " + ref.EntityID),
				Path: strings.TrimSpace(ref.URLPath), Slots: slots,
			})
		}
	}
	for _, row := range out {
		row.Count = len(row.Pages) + len(row.Instances)
	}
	return out
}

// contentTemplateSlotsLabel 把槽位名折成 LabelPair 列表：取词归模板，
// 控制器只交「这是哪个槽位」的事实。未知槽位用原值作兜底。
func contentTemplateSlotsLabel(slots []string) []contenttemplateenums.LabelPair {
	if len(slots) == 0 {
		return nil
	}
	out := make([]contenttemplateenums.LabelPair, 0, len(slots))
	for _, slot := range slots {
		switch slot {
		case builder.SlotHeader:
			out = append(out, contenttemplateenums.LabelSlotHeader)
		case builder.SlotFooter:
			out = append(out, contenttemplateenums.LabelSlotFooter)
		default:
			out = append(out, contenttemplateenums.LabelPair{Fallback: slot})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fallback < out[j].Fallback })
	return out
}

func contentTemplateTypeLabel(c *gin.Context, entityType string) string {
	return contentTemplateLabel(c, contenttemplateenums.EntityTypeLabel(entityType))
}

func contentTemplateRoleLabel(c *gin.Context, role string) string {
	return contentTemplateLabel(c, contenttemplateenums.TemplateRoleLabel(role))
}

// contentTemplateImpactNote 返回引用面提示的**事实**（key + 兜底 + 参数）；
// 取词归模板，Go 不拼成品句。
func contentTemplateImpactNote(impact *contenttemplatedto.ImpactResp, err error) contentTemplateMsg {
	if err != nil {
		return contentTemplateInternalMsg()
	}
	if impact == nil || !impact.Available {
		return contentTemplateImpactUnavailableMsg
	}
	if impact.Unparsable > 0 {
		msg := contentTemplateImpactUnparsableMsg
		msg.Args = []string{fmt.Sprint(impact.Unparsable)}
		return msg
	}
	return contentTemplateMsg{}
}

func toTemplatePageRefs(in []contentTemplatePageRef) []contenttemplatedto.TemplatePageRefResp {
	out := make([]contenttemplatedto.TemplatePageRefResp, 0, len(in))
	for _, r := range in {
		out = append(out, contenttemplatedto.TemplatePageRefResp{
			Label: r.Label, Path: r.Path, Slots: r.Slots, URL: r.URL,
		})
	}
	return out
}

func toTemplateInstanceRefs(in []contentTemplateInstanceRef) []contenttemplatedto.TemplateInstanceRefResp {
	out := make([]contenttemplatedto.TemplateInstanceRefResp, 0, len(in))
	for _, r := range in {
		out = append(out, contenttemplatedto.TemplateInstanceRefResp{
			Label: r.Label, Entity: r.Entity, Path: r.Path, Slots: r.Slots,
		})
	}
	return out
}
