package purchasehttp

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	"go_wp/pkg/binding"
	bulkids "go_wp/pkg/bulkids"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	pageview "go_wp/pkg/view"
)

const (
	sourceStatusFilterAll = "all"

	sourcePageSize = 200

	sourceMaxPageSize = 200

	sourcesPath = "/admin/inventory/sources"
)

type sourcePageHandle struct {
	purchase purchasecontract.PurchaseService
	projects projectcontract.ProjectService
}

func NewSourcePageHandle(purchase purchasecontract.PurchaseService,
	projects projectcontract.ProjectService) *sourcePageHandle {
	return &sourcePageHandle{purchase: purchase, projects: projects}
}

type sourcesPageData struct {
	Title           string `view:"title"`
	Menu            string `view:"menu"`
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []purchasedto.SourceRowResp
	HasSummary      bool
	Summary         purchasedto.SourceSummaryView
	FilterType      string
	FilterRelated   string
	FilterStatus    string
	FilterKeyword   string

	Err string

	LoadFailed bool

	Pagination pagination.Page

	TypeOptions    []purchasedto.OptionResp
	StatusOptions  []purchasedto.OptionResp
	RelatedOptions []purchasedto.OptionResp

	FilterStatusAll string

	ListQuery string

	SourceCreateForm *purchasedto.SourceFormResp
}

func fillSourceOptions(d *sourcesPageData, c *gin.Context) *sourcesPageData {
	d.Title, d.Menu = purchaseenums.MsgInventorySourcesTitle, "inventory-sources"
	d.TypeOptions, d.StatusOptions = sourceTypeOptions(), sourceStatusOptions()
	d.RelatedOptions, d.FilterStatusAll = sourceRelatedOptions(), sourceStatusFilterAll
	d.ListQuery = binding.ListQuery(c, purchaseSourceBackKeys...)
	return d
}

func (h *sourcePageHandle) sourcesPageView(c *gin.Context, d *sourcesPageData) any {
	fillSourceOptions(d, c)
	d.SourceCreateForm = sourceFormData(c, d.SelectedProject, false, nil)
	return d
}

func (h *sourcePageHandle) SourcesPage(c *gin.Context) {
	q, ok := binding.Query[purchasedto.SourceListQueryReq](c)
	if !ok {
		return
	}
	pageview.Render(c, h.sourcesPageView(c, h.sourcesPageFacts(c, q)))
}

func (h *sourcePageHandle) sourcesPageFacts(c *gin.Context, q *purchasedto.SourceListQueryReq) *sourcesPageData {
	d, selected, ok := h.sourcePageBasics(c, q)
	if !ok {
		return d
	}
	page, limit := purchasePageParams(q.Page, q.Limit, sourcePageSize, sourceMaxPageSize)
	filter := sourcePageFilter{d: d, projectID: selected, page: page, limit: limit}
	filter.type_, filter.status = strings.TrimSpace(q.Type), strings.TrimSpace(q.Status)
	filter.relatedParty, filter.keyword = strings.TrimSpace(q.RelatedParty), strings.TrimSpace(q.Keyword)

	filter.includeDisabled = filter.status == sourceStatusFilterAll
	if filter.includeDisabled {
		filter.status = ""
	}
	h.fillSourceRows(c, &filter)
	return d
}

func (h *sourcePageHandle) sourcePageBasics(c *gin.Context, q *purchasedto.SourceListQueryReq) (
	d *sourcesPageData, selected string, ok bool) {
	projects, loadErr := h.projects.List(c.Request.Context())
	d = &sourcesPageData{
		Projects:        projects,
		Sources:         []purchasedto.SourceRowResp{},
		FilterType:      strings.TrimSpace(q.Type),
		FilterRelated:   strings.TrimSpace(q.RelatedParty),
		FilterStatus:    strings.TrimSpace(q.Status),
		FilterKeyword:   strings.TrimSpace(q.Keyword),
		FilterStatusAll: sourceStatusFilterAll,

		Summary: purchasedto.SourceSummaryView{Groups: []purchasedto.SourceSummaryGroupView{}},
	}
	if loadErr != nil {

		d.Projects, d.LoadFailed = nil, true
		d.Err = purchaseErrText(c, loadErr)
		return d, "", false
	}
	selected = strings.TrimSpace(q.ProjectID)
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	d.SelectedProject = selected
	return d, selected, true
}

type sourcePageFilter struct {
	d               *sourcesPageData
	projectID       string
	type_           string
	relatedParty    string
	status          string
	keyword         string
	includeDisabled bool
	page            int
	limit           int
}

func (h *sourcePageHandle) fillSourceRows(c *gin.Context, f *sourcePageFilter) {
	ctx := c.Request.Context()

	filterReq := &purchasedto.ListSourceReq{
		ProjectID: f.projectID, Type: f.type_, RelatedParty: f.relatedParty,
		Status: f.status, Keyword: f.keyword, IncludeDisabled: f.includeDisabled,
	}

	var total int64
	if n, cerr := h.purchase.CountSources(ctx, filterReq); cerr != nil {

		if f.d.Err == "" {
			f.d.Err = purchaseErrText(c, cerr)
		}
	} else {
		total = n
		f.page = clampPurchasePage(f.page, f.limit, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = f.page, f.limit
		list, lerr := h.purchase.ListSources(ctx, &listReq)
		if lerr != nil {
			if f.d.Err == "" {
				f.d.Err = purchaseErrText(c, lerr)
			}
		} else {
			tr := purchaseTranslate(c)
			for _, item := range list {
				f.d.Sources = append(f.d.Sources, sourceRow(tr, item))
			}
		}
	}
	f.d.Summary, f.d.HasSummary = h.sourceSummary(c, ctx, f.projectID, &f.d.Err)

	statusForLink := f.status
	if f.includeDisabled {
		statusForLink = sourceStatusFilterAll
	}
	f.d.Pagination = pagination.Page{Total: total, Page: f.page, Limit: f.limit}
	f.d.Pagination.Base = reqparams.FilterBaseURL(sourcesPath, map[string]string{
		"project":      f.projectID,
		"type":         f.type_,
		"relatedParty": f.relatedParty,
		"status":       statusForLink,
		"keyword":      f.keyword,
	})
}

func (h *sourcePageHandle) SourceCreate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceFormReq](c)
	if !ok {
		return
	}
	req, ferr := form.CreateReq()
	if ferr != nil {
		purchaseFail(c, ferr)
		return
	}
	if _, err := h.purchase.CreateSource(c.Request.Context(), req); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *sourcePageHandle) SourceUpdate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceFormReq](c)
	if !ok {
		return
	}

	req, ferr := form.UpdateReq()
	if ferr != nil {
		purchaseFail(c, ferr)
		return
	}
	if _, err := h.purchase.UpdateSource(c.Request.Context(), req); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *sourcePageHandle) SourceDelete(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceDeleteFormReq](c)
	if !ok {
		return
	}
	if err := h.purchase.DeleteSource(c.Request.Context(), &purchasedto.DeleteSourceReq{
		ID: form.ID,
	}); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *sourcePageHandle) SourcesBulkDelete(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceBulkFormReq](c)
	if !ok {
		return
	}

	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {

		purchaseFailMsg(c, purchaseBulkIDsMsg(c, berr))
		return
	}
	if err := h.purchase.BulkDeleteSources(c.Request.Context(), &purchasedto.BulkDeleteSourceReq{
		IDs: ids, ProjectID: form.ProjectID,
	}); err != nil {
		pageview.FailScene(c, purchaseErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *sourcePageHandle) sourceSummary(c *gin.Context, ctx context.Context, projectID string, pageErr *string) (out purchasedto.SourceSummaryView, ok bool) {
	out = purchasedto.SourceSummaryView{Groups: []purchasedto.SourceSummaryGroupView{}}
	if projectID == "" {
		return out, false
	}
	res, err := h.purchase.SourceSummary(ctx, &purchasedto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {

		if *pageErr == "" {
			*pageErr = purchaseErrText(c, err)
		}
		return out, false
	}
	groups := make([]purchasedto.SourceSummaryGroupView, 0, len(res.Groups))
	tr := purchaseTranslate(c)
	for _, g := range res.Groups {
		groups = append(groups, purchasedto.SourceSummaryGroupView{
			Type: g.Type, TypeLabel: sourceTypeLabel(tr, g.Type),
			RelatedParty: g.RelatedParty, RelatedPartyLabel: sourceRelatedLabel(tr, g.RelatedParty),
			Count: g.Count,
		})
	}
	return purchasedto.SourceSummaryView{
		Total: res.Total, Internal: res.Internal, External: res.External,
		RelatedParty: res.RelatedParty, Unrelated: res.Unrelated,
		SettlePriced: res.SettlePriced, Groups: groups,
	}, true
}

func sourceRow(tr func(key, fallback string) string, s *purchasedto.SourceResp) purchasedto.SourceRowResp {
	settlePrice := "—"
	if s.SettlePrice != nil {
		settlePrice = strconv.FormatFloat(*s.SettlePrice, 'f', 2, 64)
	}
	return purchasedto.SourceRowResp{
		ID: s.ID, Code: s.Code, Name: s.Name,
		Type: s.Type, TypeLabel: sourceTypeLabel(tr, s.Type),
		RelatedParty: s.RelatedParty, RelatedPartyLabel: sourceRelatedLabel(tr, s.RelatedParty),
		Status: s.Status, StatusLabel: statusLabel(tr, s.Status),
		SettlePrice: settlePrice, HasSettlePrice: s.SettlePrice != nil,
		Config: prettyJSON(s.Config), Sort: s.Sort, UpdatedAt: s.UpdatedAt,
	}
}

// 选项只交「值 + 固定词条 key + 兜底」；取词归模板。

func sourceTypeOptions() []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: purchaseenums.SourceTypeExternal},
		{Value: purchaseenums.SourceTypeInternal},
	}
}

func sourceStatusOptions() []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: purchaseenums.SourceStatusActive},
		{Value: purchaseenums.SourceStatusDisabled},
	}
}

func sourceRelatedOptions() []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: ""},
		{Value: "true"},
		{Value: "false"},
	}
}

func sourceTypeLabel(tr func(key, fallback string) string, sourceType string) string {
	switch sourceType {
	case purchaseenums.SourceTypeInternal:
		return tr(purchaseenums.KeySourcesTypeInternal, "内部（集团内 / 自家工厂）")
	case purchaseenums.SourceTypeExternal:
		return tr(purchaseenums.KeySourcesStatsExternal, "外部供应商")
	default:
		return sourceType
	}
}

func sourceRelatedLabel(tr func(key, fallback string) string, related bool) string {
	if related {
		return tr(purchaseenums.KeySourcesStatsRelated, "关联方")
	}
	return tr(purchaseenums.KeySourcesStatsUnrelated, "非关联方")
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

func sourceFormData(c *gin.Context, projectID string, edit bool, row any) *purchasedto.SourceFormResp {
	return &purchasedto.SourceFormResp{
		IsEdit: edit, Row: row, SelectedProject: projectID,
		TypeOptions: sourceTypeOptions(), RelatedOptions: sourceRelatedOptions(),
		StatusOptions: sourceStatusOptions(),
	}
}
