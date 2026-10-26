// 货源后台页与货源写动作（新建 / 编辑 / 删除 / 批量删除、汇总卡片）。
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
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const (
	// sourceStatusFilterAll 状态筛选的「全部（含停用）」取值。
	//
	// 用 all 而不是让空值同时表示「全部」：否则「默认视图」（只列启用中）与
	// 「全量视图」无法区分。
	sourceStatusFilterAll = "all"
	// sourcePageSize 货源页一次列出的条数（每页条数；完整清单靠分页翻）。
	sourcePageSize = 200
	// sourceMaxPageSize 货源页每页条数的上限（?limit= 的封顶值）。
	//
	// 与 service 的 maxSourcePageSize 同口径：超过它 service 会自己截到 200，
	// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 这类缺陷页面不报错，只是数据对不上。
	sourceMaxPageSize = 200
	// sourcesPath 货源页路径（回跳与分页基址的唯一定义处）。
	sourcesPath = "/admin/inventory/sources"
)

// sourcePageHandle 货源管理页处理器。
type sourcePageHandle struct {
	purchase purchasecontract.PurchaseService
	projects projectcontract.ProjectService
}

// NewSourcePageHandle 构造。
func NewSourcePageHandle(purchase purchasecontract.PurchaseService,
	projects projectcontract.ProjectService) *sourcePageHandle {
	return &sourcePageHandle{purchase: purchase, projects: projects}
}

// sourcesPageData 货源管理页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 单独一个类型的理由与 project 域主题管理页同源：降级渲染若另抄一份 gin.H，两处的键集
// 必然分叉 —— 而 Jet 的可选键缺 key 是**整页中断**（HTTP 200 + 后面整块 HTML 消失），
// 症状比脱壳的纯文本更难看出来。
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
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 货源列表 / 统计）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 模板里没有可靠的办法分辨「Sources 为空」是「这个工程真的没有货源」还是
	// 「这一次没读出来」—— 前者要引导去建一个，后者只能说明「稍后重试」。
	LoadFailed bool
	// Pagination 分页条事实（零值 = 单页 / 装载失败，HasPagination 为假，模板不渲染）。
	Pagination pagination.Page

	// 三个下拉的文案（按请求语言）：取值需要 c，由装配处填。
	TypeOptions    []purchasedto.OptionResp
	StatusOptions  []purchasedto.OptionResp
	RelatedOptions []purchasedto.OptionResp
	// FilterStatusAll 「全部状态」这一档的值（与 status 选项区分：它不是一个状态）。
	FilterStatusAll string
	// ListQuery 写动作表单 action 上带的筛选上下文。
	ListQuery string
	// SourceCreateForm 新建抽屉的初值（与页数据同一个视图：模板直接读 .SourceCreateForm.X）。
	SourceCreateForm *purchasedto.SourceFormResp
}

// fillSourceOptions 填装配期才知道的键（取词 / 筛选上下文）。
//
// 单独一个函数而不是内联：**装配与渲染是两件事** —— 列表数据的取数在 SourcesPage，
// 而这几项只依赖 c。放在一起会让那个 handler 再涨一截语句数（门禁按「控制器只做编排」卡它）。
func fillSourceOptions(d *sourcesPageData, c *gin.Context) *sourcesPageData {
	tr := purchaseTranslate(c)
	d.Title, d.Menu = purchaseenums.MsgInventorySourcesTitle, "inventory-sources"
	d.TypeOptions, d.StatusOptions = sourceTypeOptions(tr), sourceStatusOptions(tr)
	d.RelatedOptions, d.FilterStatusAll = sourceRelatedOptions(tr), sourceStatusFilterAll
	d.ListQuery = binding.ListQuery(c, purchaseSourceBackKeys...)
	return d
}

// sourcesPageView 货源页的视图：正常与降级两条路都从这里出，
// 键集只有一处定义（降级分支不必「记得」补齐模板要的每一个键）。
func (h *sourcePageHandle) sourcesPageView(c *gin.Context, d *sourcesPageData) any {
	fillSourceOptions(d, c)
	d.SourceCreateForm = sourceFormData(c, d.SelectedProject, false, nil)
	return d
}

// SourcesPage 货源管理页：工程切换 + 关联方统计 + 筛选 + 新建 + 列表（可编辑）。
func (h *sourcePageHandle) SourcesPage(c *gin.Context) {
	q, ok := binding.Query[purchasedto.SourceListQueryReq](c)
	if !ok {
		return
	}
	pageview.Render(c, h.sourcesPageView(c, h.sourcesPageFacts(c, q)))
}

// sourcesPageFacts 采集本页要展示的事实（列表 / 汇总 / 分页 / 装载失败）。
//
// 与采购页同形：门禁按「控制器只做绑定 → 调业务层 → 出口」卡语句数，
// 取数与装配留在出口外的这个方法里，两条渲染路径（正常 / 降级）共用同一份结果。
func (h *sourcePageHandle) sourcesPageFacts(c *gin.Context, q *purchasedto.SourceListQueryReq) *sourcesPageData {
	d, selected, ok := h.sourcePageBasics(c, q)
	if !ok {
		return d
	}
	page, limit := purchasePageParams(q.Page, q.Limit, sourcePageSize, sourceMaxPageSize)
	filter := sourcePageFilter{d: d, projectID: selected, page: page, limit: limit}
	filter.type_, filter.status = strings.TrimSpace(q.Type), strings.TrimSpace(q.Status)
	filter.relatedParty, filter.keyword = strings.TrimSpace(q.RelatedParty), strings.TrimSpace(q.Keyword)
	// 状态筛选三态：""（默认只列启用中）/ active / disabled / all（连停用的一起列）。
	filter.includeDisabled = filter.status == sourceStatusFilterAll
	if filter.includeDisabled {
		filter.status = ""
	}
	h.fillSourceRows(c, &filter)
	return d
}

// sourcePageBasics 页头（工程上下文）与「装载失败」判定。
//
// 降级渲染的判据只有一处：工程列表读不出来就不再去读列表与统计 ——
// 工程没定下来时查出来的货源属于哪个工程都说不清，比空列表更误导。
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
		// 与 sourceSummary 的失败分支同形：模板只在 HasSummary 为真时读 Summary，
		// 但键本身必须在（组内键缺失同样会中断渲染）。
		Summary: purchasedto.SourceSummaryView{Groups: []purchasedto.SourceSummaryGroupView{}},
	}
	if loadErr != nil {
		// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
		// 页头 / 筛选栏 / 批量条与侧栏全部保留 —— 运营看得出「是这一页没读出来」。
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

// sourcePageFilter 货源列表的筛选与分页（四个维度 + 页码 + 每页条数）。
//
// 单独一个结构体而不是六个参数：这些值要同时喂给 CountSources 与 ListSources
// （两者必须同源，否则分页条会凭空多出一页空列表）。
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

// fillSourceRows 取当页货源、汇总卡与分页条。
func (h *sourcePageHandle) fillSourceRows(c *gin.Context, f *sourcePageFilter) {
	ctx := c.Request.Context()
	// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size。
	// 两处各写一份时，最容易漏的是 IncludeDisabled 决定的那一档默认状态 ——
	// 计数把停用的也算进去，分页条就会凭空多出一页空列表。
	filterReq := &purchasedto.ListSourceReq{
		ProjectID: f.projectID, Type: f.type_, RelatedParty: f.relatedParty,
		Status: f.status, Keyword: f.keyword, IncludeDisabled: f.includeDisabled,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampPurchasePage）。
	var total int64
	if n, cerr := h.purchase.CountSources(ctx, filterReq); cerr != nil {
		// 筛选参数不合法等：把业务错误回显到页面，不把内部细节直出。
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

	// 分页条（零值 = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。
	// status 用**原始取值**：includeDisabled 时 FilterStatus 已被归一成空串（模板的
	// 「全部（含停用）」是另一个键 FilterStatusAll），拿归一后的值拼链接会让翻页时
	// 「连停用的一起列」被悄悄丢掉 —— 表现为翻页后记录变少，看着像数据丢了。
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

// SourceCreate 新建货源（内部类型自动成为关联方）。
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
	response.Success(c, nil)
}

// SourceUpdate 修改货源（表单即最终状态：结算价留空 = 清空）。
func (h *sourcePageHandle) SourceUpdate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceFormReq](c)
	if !ok {
		return
	}
	// 表单是最终状态：留空即「这条货源没有结算价」，由 DTO 显式翻成 ClearSettlePrice。
	req, ferr := form.UpdateReq()
	if ferr != nil {
		purchaseFail(c, ferr)
		return
	}
	if _, err := h.purchase.UpdateSource(c.Request.Context(), req); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// SourceDelete 删除货源。
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
	response.Success(c, nil)
}

// SourcesBulkDelete 批量删除货源。
//
// 逐条走同一条删除路径：被采购单等历史数据引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 部分成功给的是「已删 N 个 / 跳过 M 个」这一句（400 + JSON，由前端 toast），避免静默。
func (h *sourcePageHandle) SourcesBulkDelete(c *gin.Context) {
	form, ok := binding.Form[purchasedto.SourceBulkFormReq](c)
	if !ok {
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {
		// 超限是**受控错误**（pkg/bulkids 的 *IDsError：值域只有 Count/Max 两个整数，
		// 装不下表名 / 约束名 / SQLSTATE），文案走受控结论出口 —— 它按当前语言给
		//「一次最多操作 N 项，当前 M 项，请分批进行」，不会把可行动提示抹成通用提示。
		purchaseFailMsg(c, purchaseBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.purchase.DeleteSource(c.Request.Context(), &purchasedto.DeleteSourceReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	switch {
	case skipped > 0:
		purchaseFailMsg(c, purchaseBulkSourcePartial.WithArgs(strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		response.Success(c, nil)
	default:
		// 没有选中任何 id（批量条在无勾选时不会提交）：回列表。
		response.Success(c, nil)
	}
}

// sourceSummary 关联方统计。统计失败不阻断页面：置空并附带提示。
// c 是给「错误文案三件套」用的（翻译 + 记日志时的 user_id）：取数失败要走 purchaseErrText，
// 而不是把 err.Error() 铺进模板。
func (h *sourcePageHandle) sourceSummary(c *gin.Context, ctx context.Context, projectID string, pageErr *string) (out purchasedto.SourceSummaryView, ok bool) {
	out = purchasedto.SourceSummaryView{Groups: []purchasedto.SourceSummaryGroupView{}}
	if projectID == "" {
		return out, false
	}
	res, err := h.purchase.SourceSummary(ctx, &purchasedto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		// 跨模块取数失败也可能是基础设施错误（表名 / SQLSTATE），走同一套归口，
		// 绝不用 err.Error() 直接铺到页面上。
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

// sourceRow 货源行 → 模板视图（金额与配置都先格式化，模板不做逻辑）。
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

// sourceTypeOptions 类型下拉（外部 / 内部）。
func sourceTypeOptions(tr func(key, fallback string) string) []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: purchaseenums.SourceTypeExternal, Label: sourceTypeLabel(tr, purchaseenums.SourceTypeExternal)},
		{Value: purchaseenums.SourceTypeInternal, Label: sourceTypeLabel(tr, purchaseenums.SourceTypeInternal)},
	}
}

// sourceStatusOptions 状态下拉（启用 / 停用）。
func sourceStatusOptions(tr func(key, fallback string) string) []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: purchaseenums.SourceStatusActive, Label: statusLabel(tr, purchaseenums.SourceStatusActive)},
		{Value: purchaseenums.SourceStatusDisabled, Label: statusLabel(tr, purchaseenums.SourceStatusDisabled)},
	}
}

// sourceRelatedOptions 关联方三态下拉：空值 = 按类型默认（编辑时 = 不改）。
func sourceRelatedOptions(tr func(key, fallback string) string) []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: "", Label: tr(purchaseenums.KeySourcesRelatedTypeDefault, "按类型默认 / 不改")},
		{Value: "true", Label: sourceRelatedLabel(tr, true)},
		{Value: "false", Label: sourceRelatedLabel(tr, false)},
	}
}

// sourceTypeLabel 货源类型 → 当前语言展示文案。
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

// sourceRelatedLabel 关联方标志 → 当前语言展示文案。
func sourceRelatedLabel(tr func(key, fallback string) string, related bool) string {
	if related {
		return tr(purchaseenums.KeySourcesStatsRelated, "关联方")
	}
	return tr(purchaseenums.KeySourcesStatsUnrelated, "非关联方")
}

// prettyJSON 把 jsonb 缩进成可读文本（解析失败时原样回填，不吞掉用户数据）。
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

// sourceFormData 货源抽屉表单的初值（新建与编辑共用）。
func sourceFormData(c *gin.Context, projectID string, edit bool, row any) *purchasedto.SourceFormResp {
	tr := purchaseTranslate(c)
	return &purchasedto.SourceFormResp{
		IsEdit: edit, Row: row, SelectedProject: projectID,
		TypeOptions: sourceTypeOptions(tr), RelatedOptions: sourceRelatedOptions(tr),
		StatusOptions: sourceStatusOptions(tr),
	}
}
