// 货源后台页与货源写动作（新建 / 编辑 / 删除 / 批量删除、汇总卡片）。
package inventoryhttp

import (
	"bytes"
	"context"
	"github.com/gin-gonic/gin"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"strconv"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	bulkids "go_wp/pkg/bulkids"
	json "go_wp/pkg/jsonx"
)

// inventorySourcePageHandle 货源管理页处理器。
type inventorySourcePageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
}

// NewInventorySourcePageHandle 构造。
func NewInventorySourcePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService) *inventorySourcePageHandle {
	return &inventorySourcePageHandle{inventory: inventory, projects: projects}
}

// inventorySourcesPageData 货源管理页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 单独一个类型的理由与 project 域主题管理页同源：降级渲染若另抄一份 gin.H，两处的键集
// 必然分叉 —— 而 Jet 的可选键缺 key 是**整页中断**（HTTP 200 + 后面整块 HTML 消失），
// 症状比脱壳的纯文本更难看出来。
type inventorySourcesPageData struct {
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []inventorydto.SourceRowResp
	HasSummary      bool
	Summary         inventorydto.SourceSummaryView
	FilterType      string
	FilterRelated   string
	FilterStatus    string
	FilterKeyword   string
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 货源列表 / 统计）。
	// 写动作的结论不再回带（走 shell.RenderJump，见 inventory_notice_handle.go）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与 order 域订单页 / 退货页同名的判据：模板里没有可靠的办法分辨「Sources 为空」
	// 是「这个工程真的没有货源」还是「这一次没读出来」—— 前者要引导去建一个，
	// 后者只能说明「稍后重试」，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条事实（零值 = 单页 / 装载失败，HasPagination 为假，模板不渲染）。
	//
	// 与其它键一样放在结构体里：正常渲染与降级渲染共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断，见 internal/templates/CLAUDE.md）。
	Pagination pagination.Page
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
//
// c 只用于取词：下拉项文案按请求语言渲染（取词一律经 tr(key, 中文兜底)）。
func (d *inventorySourcesPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
	m := gin.H{
		"title":           inventoryenums.MsgInventorySourcesTitle,
		"menu":            "inventory-sources",
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Sources":         d.Sources,
		"HasSummary":      d.HasSummary,
		"Summary":         d.Summary,
		"TypeOptions":     sourceTypeOptions(tr),
		"StatusOptions":   sourceStatusOptions(tr),
		"RelatedOptions":  sourceRelatedOptions(tr),
		"FilterType":      d.FilterType,
		"FilterRelated":   d.FilterRelated,
		"FilterStatus":    d.FilterStatus,
		"FilterStatusAll": sourceStatusFilterAll,
		"FilterKeyword":   d.FilterKeyword,
		"Err":             d.Err,
		"LoadFailed":      d.LoadFailed,
		// 写动作表单 action 上带的筛选上下文（见 inventory_notice_handle.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventorySourceBackKeys...),
	}
	// 分页条事实交给 partial 决定：零值（单页）与装载失败两条路 HasPagination 都为假，
	// 片段自然不渲染。
	m["Pagination"] = d.Pagination
	return m
}

// sourcesPageView 货源页的视图：正常与降级两条路都从这里出，
// 键集只有一处定义（降级分支不必「记得」补齐模板要的每一个键）。
func (h *inventorySourcePageHandle) sourcesPageView(c *gin.Context, d *inventorySourcesPageData) gin.H {
	data := d.templateMap(c)
	data["SourceCreateForm"] = sourceFormData(c, d.SelectedProject, false, nil)
	return data
}

// InventorySourcesPage 货源管理页：工程切换 + 关联方统计 + 筛选 + 新建 + 列表（可编辑）。
func (h *inventorySourcePageHandle) InventorySourcesPage(c *gin.Context) (any, error) {
	ctx := c.Request.Context()

	// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_notice_handle.go），
	// 所以提示条只剩一处来源：本页取数失败（工程列表 / 货源列表 / 统计）。
	pageErr := ""

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与 order 域订单页 / project 域主题页一致）：
	// 空列表 + 归口提示 + HTTP 200，页头 / 筛选栏 / 批量条与侧栏全部保留 ——
	// 运营看得出「是这一页没读出来」，而不是对着一块纯文本以为整个后台坏了。
	//
	// 原先这里是 `c.String(500, adminui.MsgInternalError)`：响应的是一块**裸归口 key** 的纯文本，
	// 页面上显示的就是 `MsgInternalError` 这串英文（既没翻译、也没页壳）。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = inventoryErrText(c, loadErr)
	}

	// 装载失败时不再去读列表与统计：工程上下文没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查货源，查出来的是哪个工程的货源都说不清。
	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	filterType := strings.TrimSpace(c.Query("type"))
	filterRelated := strings.TrimSpace(c.Query("relatedParty"))
	filterStatus := strings.TrimSpace(c.Query("status"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	// 状态筛选三态：""（默认只列启用中）/ active / disabled / all（连停用的一起列）。
	// 用 all 而不是让空值同时表示「全部」，否则「默认视图」与「全量视图」无法区分。
	includeDisabled := filterStatus == sourceStatusFilterAll
	if includeDisabled {
		filterStatus = ""
	}

	sources := []inventorydto.SourceRowResp{}
	// 与 sourceSummary 的失败分支同形：模板只在 HasSummary 为真时读 Summary，
	// 但键本身必须在（组内键缺失同样会中断渲染）。
	summary := inventorydto.SourceSummaryView{Groups: []inventorydto.SourceSummaryGroupView{}}
	hasSummary := false
	var total int64
	// 分页（审计 D3）：?page= / ?limit=，缺省每页 200 条。原先这一页写死 200 且没有分页条，
	// 第 201 个货源**静默消失**、页面上没有任何提示。
	page, limit := inventoryPageParams(c, inventorySourcePageSize, inventorySourceMaxPageSize)
	if !loadFailed {
		// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size。
		// 两处各写一份时，最容易漏的是 IncludeDisabled 决定的那一档默认状态 ——
		// 计数把停用的也算进去，分页条就会凭空多出一页空列表。
		filterReq := &inventorydto.ListSourceReq{
			ProjectID: selected, Type: filterType, RelatedParty: filterRelated,
			Status: filterStatus, Keyword: filterKeyword,
			IncludeDisabled: includeDisabled,
		}
		// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）。
		if n, cerr := h.inventory.CountSources(ctx, filterReq); cerr != nil {
			// 筛选参数不合法等：把业务错误回显到页面，不把内部细节直出。
			// 统一走库存域的「错误文案三件套」：命中白名单 → 原样业务文案；否则记结构化日志 + 归口文案
			//（模板只渲染这一份成品文案，不再对 .Err 二次取词）。
			if pageErr == "" {
				pageErr = inventoryErrText(c, cerr)
			}
		} else {
			total = n
			page = clampInventoryPage(page, limit, total)
			listReq := *filterReq
			listReq.Page, listReq.Size = page, limit
			list, lerr := h.inventory.ListSources(ctx, &listReq)
			if lerr != nil {
				if pageErr == "" {
					pageErr = inventoryErrText(c, lerr)
				}
			} else {
				tr := shell.TranslateFor(c)
				for _, s := range list {
					sources = append(sources, sourceRow(tr, s))
				}
			}
		}
		summary, hasSummary = h.sourceSummary(c, ctx, selected, &pageErr)
	}

	// 分页条（nil = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。
	// status 用**原始取值**：includeDisabled 时 FilterStatus 已被归一成空串（模板的
	// 「全部（含停用）」是另一个键 FilterStatusAll），拿归一后的值拼链接会让翻页时
	// 「连停用的一起列」被悄悄丢掉 —— 表现为翻页后记录变少，看着像数据丢了。
	var pg pagination.Page
	if !loadFailed {
		statusForLink := filterStatus
		if includeDisabled {
			statusForLink = sourceStatusFilterAll
		}
		pg = pagination.Facts(total, page, limit)
		pg.Base = reqparams.FilterBaseURL("/admin/inventory/sources", map[string]string{
			"project":      selected,
			"type":         filterType,
			"relatedParty": filterRelated,
			"status":       statusForLink,
			"keyword":      filterKeyword,
		})
	}

	return h.sourcesPageView(c, &inventorySourcesPageData{
		Projects:        projects,
		SelectedProject: selected,
		Sources:         sources,
		HasSummary:      hasSummary,
		Summary:         summary,
		FilterType:      filterType,
		FilterRelated:   filterRelated,
		FilterStatus:    filterStatus,
		FilterKeyword:   filterKeyword,
		Err:             pageErr,
		LoadFailed:      loadFailed,
		Pagination:      pg,
	}), nil
}

// InventorySourceCreate 新建货源（内部类型自动成为关联方）。
func (h *inventorySourcePageHandle) InventorySourceCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.SourceFormReq](c)
	if !ok {
		return
	}
	req, ferr := form.CreateReq()
	if ferr != nil {
		h.sourceFormFail(c, ferr)
		return
	}
	if _, err := h.inventory.CreateSource(c.Request.Context(), req); err != nil {
		h.sourceFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventorySourceBack(c), inventorySourceBackMsg)
}

// InventorySourceUpdate 修改货源（表单即最终状态：结算价留空 = 清空）。
func (h *inventorySourcePageHandle) InventorySourceUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.SourceFormReq](c)
	if !ok {
		return
	}
	// 表单是最终状态：留空即「这条货源没有结算价」，由 DTO 显式翻成 ClearSettlePrice。
	req, ferr := form.UpdateReq()
	if ferr != nil {
		h.sourceFormFail(c, ferr)
		return
	}
	if _, err := h.inventory.UpdateSource(c.Request.Context(), req); err != nil {
		h.sourceFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventorySourceBack(c), inventorySourceBackMsg)
}

// InventorySourceDelete 删除货源。
func (h *inventorySourcePageHandle) InventorySourceDelete(c *gin.Context) {
	form, ok := binding.Form[inventorydto.SourceDeleteFormReq](c)
	if !ok {
		return
	}
	if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{
		ID: form.ID,
	}); err != nil {
		inventoryJump(c, false, inventoryErrMsg(c, err), inventorySourceBack(c), inventorySourceBackMsg)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventorySourceBack(c), inventorySourceBackMsg)
}

// InventorySourcesBulkDelete 批量删除货源。
//
// 逐条走同一条删除路径：被采购单等历史数据引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染成提示页，避免静默的部分成功（语义未变，只换传输通道）。
func (h *inventorySourcePageHandle) InventorySourcesBulkDelete(c *gin.Context) {
	form, ok := binding.Form[inventorydto.SourceBulkFormReq](c)
	if !ok {
		return
	}
	back := inventorySourceBack(c)
	backMsg := inventorySourceBackMsg
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {
		// 超限是**受控错误**（pkg/bulkids 的 *IDsError：值域只有 Count/Max 两个整数，
		// 装不下表名 / 约束名 / SQLSTATE），文案走受控结论出口 —— 它按当前语言给
		//「一次最多操作 N 项，当前 M 项，请分批进行」，不会把可行动提示抹成通用提示。
		inventoryJump(c, false, inventoryBulkIDsMsg(c, berr), back, backMsg)
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	switch {
	case skipped > 0:
		inventoryJump(c, false, inventoryBulkSourcePartial.WithArgs(strconv.Itoa(deleted), strconv.Itoa(skipped)), back, backMsg)
	case deleted > 0:
		inventoryJump(c, true, inventoryBulkSourceDone.WithArgs(strconv.Itoa(deleted)), back, backMsg)
	default:
		// 没有选中任何 id（批量条在无勾选时不会提交）：回列表，不渲染提示页。
		redirectWhere(c, back)
	}
}

// sourceSummary 关联方统计（验收 4）。统计失败不阻断页面：置空并附带提示。
// c 是给「错误文案三件套」用的（翻译 + 记日志时的 user_id）：取数失败要走 inventoryErrText，
// 而不是把 err.Error() 铺进模板。
func (h *inventorySourcePageHandle) sourceSummary(c *gin.Context, ctx context.Context, projectID string, pageErr *string) (out inventorydto.SourceSummaryView, ok bool) {
	out = inventorydto.SourceSummaryView{Groups: []inventorydto.SourceSummaryGroupView{}}
	if projectID == "" {
		return out, false
	}
	res, err := h.inventory.SourceSummary(ctx, &inventorydto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		// 同上：跨模块取数失败也可能是基础设施错误（表名 / SQLSTATE），走同一套归口，
		// 绝不用 err.Error() 直接铺到页面上。
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out, false
	}
	groups := make([]inventorydto.SourceSummaryGroupView, 0, len(res.Groups))
	tr := shell.TranslateFor(c)
	for _, g := range res.Groups {
		groups = append(groups, inventorydto.SourceSummaryGroupView{
			Type: g.Type, TypeLabel: sourceTypeLabel(tr, g.Type),
			RelatedParty: g.RelatedParty, RelatedPartyLabel: sourceRelatedLabel(tr, g.RelatedParty),
			Count: g.Count,
		})
	}
	return inventorydto.SourceSummaryView{
		Total: res.Total, Internal: res.Internal, External: res.External,
		RelatedParty: res.RelatedParty, Unrelated: res.Unrelated,
		SettlePriced: res.SettlePriced, Groups: groups,
	}, true
}

// sourceRow 货源行 → 模板视图（金额与配置都先格式化，模板不做逻辑）。
func sourceRow(tr func(key, fallback string) string, s *inventorydto.SourceResp) inventorydto.SourceRowResp {
	settlePrice := "—"
	if s.SettlePrice != nil {
		settlePrice = strconv.FormatFloat(*s.SettlePrice, 'f', 2, 64)
	}
	return inventorydto.SourceRowResp{
		ID: s.ID, Code: s.Code, Name: s.Name,
		Type: s.Type, TypeLabel: sourceTypeLabel(tr, s.Type),
		RelatedParty: s.RelatedParty, RelatedPartyLabel: sourceRelatedLabel(tr, s.RelatedParty),
		Status: s.Status, StatusLabel: statusLabel(tr, s.Status),
		SettlePrice: settlePrice, HasSettlePrice: s.SettlePrice != nil,
		Config: prettyJSON(s.Config), Sort: s.Sort, UpdatedAt: s.UpdatedAt,
	}
}

// sourceTypeOptions 类型下拉（外部 / 内部）。
func sourceTypeOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: inventoryenums.SourceTypeExternal, Label: sourceTypeLabel(tr, inventoryenums.SourceTypeExternal)},
		{Value: inventoryenums.SourceTypeInternal, Label: sourceTypeLabel(tr, inventoryenums.SourceTypeInternal)},
	}
}

// sourceStatusOptions 状态下拉（启用 / 停用）。
func sourceStatusOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: inventoryenums.SourceStatusActive, Label: statusLabel(tr, inventoryenums.SourceStatusActive)},
		{Value: inventoryenums.SourceStatusDisabled, Label: statusLabel(tr, inventoryenums.SourceStatusDisabled)},
	}
}

// sourceRelatedOptions 关联方三态下拉：空值 = 按类型默认（编辑时 = 不改）。
func sourceRelatedOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: "", Label: tr(inventoryenums.InventorySourcesRelatedTypeDefault, "按类型默认 / 不改")},
		{Value: "true", Label: sourceRelatedLabel(tr, true)},
		{Value: "false", Label: sourceRelatedLabel(tr, false)},
	}
}

// sourceTypeLabel 货源类型 → 当前语言展示文案。
func sourceTypeLabel(tr func(key, fallback string) string, sourceType string) string {
	switch sourceType {
	case inventoryenums.SourceTypeInternal:
		return tr(inventoryenums.InventorySourcesTypeInternal, "内部（集团内 / 自家工厂）")
	case inventoryenums.SourceTypeExternal:
		return tr(inventoryenums.InventorySourcesStatsExternal, "外部供应商")
	default:
		return sourceType
	}
}

// sourceRelatedLabel 关联方标志 → 当前语言展示文案。
func sourceRelatedLabel(tr func(key, fallback string) string, related bool) string {
	if related {
		return tr(inventoryenums.InventorySourcesStatsRelated, "关联方")
	}
	return tr(inventoryenums.InventorySourcesStatsUnrelated, "非关联方")
}

// sourceRelatedForm 解析关联方下拉：空串 = 未指定（新建按类型默认 / 编辑不改）。
func sourceRelatedForm(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		yes := true
		return &yes
	case "false", "0":
		no := false
		return &no
	default:
		return nil
	}
}

// sourceSettleForm 解析结算价输入：空串 = 无值（更新路径据此清空）。
func sourceSettleForm(value string) (out *float64, err error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	parsed, perr := strconv.ParseFloat(raw, 64)
	if perr != nil {
		return nil, inventoryenums.Err(inventoryenums.ErrSourceSettleInvalid)
	}
	return &parsed, nil
}

// sourceConfigForm 解析对接配置输入：空串表示未填（service 落成 {}）。
func sourceConfigForm(value string) json.RawMessage {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
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

func sourceFormData(c *gin.Context, projectID string, edit bool, row gin.H) gin.H {
	tr := shell.TranslateFor(c)
	return gin.H{
		"IsEdit": edit, "Row": row, "SelectedProject": projectID,
		"TypeOptions": sourceTypeOptions(tr), "RelatedOptions": sourceRelatedOptions(tr),
		"StatusOptions": sourceStatusOptions(tr),
	}
}

func (h *inventorySourcePageHandle) sourceFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrMsg(c, err), inventorySourceBack(c), inventorySourceBackMsg)
		return
	}
	// htmx 分支：4xx + JSON —— 抽屉不 swap，输入原样保留，admin.js 弹 toast。
	inventoryFailDrawer(c, err)
}