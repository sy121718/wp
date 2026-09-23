// inventory_source_page_handle.go — 后台货源管理页（issue #17）。
//
// 与库存管理页同一模式：GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域），错误经 ?err= 回显。
//
// 页面承担四条验收：
//
//	· 验收 1：可建货源，类型区分内部与外部，可标记关联方；
//	· 验收 2：货源可配置异构的对接扩展信息（config，JSON 对象）；
//	· 验收 3：后台可管理货源（建 / 改 / 停启用 / 删 / 筛选）；
//	· 验收 4：关联方标志可用于报表区分 —— 顶部给出「类型 × 关联方」的交叉统计，
//	  筛选栏的关联方维度直接落到查询上。
//
// 表单提交语义：这一页的编辑表单就是**最终状态**（字段全部回填），
// 因此「结算价留空」= 清空结算价，而不是「本次不改」。
package inventoryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// sourceStatusFilterAll 状态筛选的「全部（含停用）」取值。
const sourceStatusFilterAll = "all"

// inventorySourcePageSize 货源页一次列出的条数（每页条数；完整清单靠分页翻）。
//
// 原先它同时充当「硬编码上限」—— 第 201 个货源静默消失且页面上没有任何提示。
// 现在它是**每页条数**（可用 ?limit= 调小，上限见 inventorySourceMaxPageSize），
// 页面下方给分页条，第 201 个之后靠翻页看到。
const inventorySourcePageSize = 200

// inventorySourceMaxPageSize 货源页每页条数的上限（?limit= 的封顶值）。
//
// 与 service 的 maxSourcePageSize 同口径：超过它 service 会自己截到 200，
// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
// 页面不报错，只是数据对不上，最难查。
const inventorySourceMaxPageSize = 200

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
	Sources         []gin.H
	HasSummary      bool
	Summary         gin.H
	FilterType      string
	FilterRelated   string
	FilterStatus    string
	FilterKeyword   string
	Err             string
	Ok              string
	Done            string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与 order 域订单页 / 退货页同名的判据：模板里没有可靠的办法分辨「Sources 为空」
	// 是「这个工程真的没有货源」还是「这一次没读出来」—— 前者要引导去建一个，
	// 后者只能说明「稍后重试」，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条数据（nil = 单页 / 装载失败，模板不渲染分页条）。
	//
	// 与其它键一样放在结构体里：正常渲染与降级渲染共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断，见 internal/templates/CLAUDE.md）。
	Pagination *shell.PaginationData
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
func (d *inventorySourcesPageData) templateMap() gin.H {
	m := gin.H{
		"title":           inventoryenums.MsgInventorySourcesTitle,
		"menu":            "inventory-sources",
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Sources":         d.Sources,
		"HasSummary":      d.HasSummary,
		"Summary":         d.Summary,
		"TypeOptions":     sourceTypeOptions(),
		"StatusOptions":   sourceStatusOptions(),
		"RelatedOptions":  sourceRelatedOptions(),
		"FilterType":      d.FilterType,
		"FilterRelated":   d.FilterRelated,
		"FilterStatus":    d.FilterStatus,
		"FilterStatusAll": sourceStatusFilterAll,
		"FilterKeyword":   d.FilterKeyword,
		"Err":             d.Err,
		"Ok":              d.Ok,
		"Done":            d.Done,
		"LoadFailed":      d.LoadFailed,
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderSourcesPage 货源页的唯一渲染出口：正常与降级两条路都从这里出，
// 键集只有一处定义（降级分支不必「记得」补齐模板要的每一个键）。
func (h *inventorySourcePageHandle) renderSourcesPage(c *gin.Context, d *inventorySourcesPageData) {
	c.HTML(http.StatusOK, "admin/inventory/inventory_sources.html", shell.Prepare(c, d.templateMap()))
}

// InventorySourcesPage 货源管理页：工程切换 + 关联方统计 + 筛选 + 新建 + 列表（可编辑）。
func (h *inventorySourcePageHandle) InventorySourcesPage(c *gin.Context) {
	ctx := c.Request.Context()

	// 回显文案先过读侧白名单（见 inventory_page_handle.go 的 inventoryPageErr）：查询参数不是可信边界。
	pageErr := inventoryPageErr(c)

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与 order 域订单页 / project 域主题页一致）：
	// 空列表 + 归口提示 + HTTP 200，页头 / 筛选栏 / 批量条与侧栏全部保留 ——
	// 运营看得出「是这一页没读出来」，而不是对着一块纯文本以为整个后台坏了。
	//
	// 原先这里是 `c.String(500, shell.MsgInternalError)`：响应的是一块**裸归口 key** 的纯文本，
	// 页面上显示的就是 `MsgInternalError` 这串英文（既没翻译、也没页壳）。
	//
	// 装载失败**压过 ?err=**：它是这次请求真实发生的事，URL 里那条是上一次写失败的旧提示。
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

	sources := []gin.H{}
	// 与 sourceSummary 的失败分支同形：模板只在 HasSummary 为真时读 Summary，
	// 但键本身必须在（组内键缺失同样会中断渲染）。
	summary := gin.H{"Groups": []gin.H{}}
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
				for _, s := range list {
					sources = append(sources, sourceRow(s))
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
	var pagination *shell.PaginationData
	if !loadFailed {
		statusForLink := filterStatus
		if includeDisabled {
			statusForLink = sourceStatusFilterAll
		}
		pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
			"/admin/inventory/sources", map[string]string{
				"project":      selected,
				"type":         filterType,
				"relatedParty": filterRelated,
				"status":       statusForLink,
				"keyword":      filterKeyword,
			}), shell.TranslateFor(c))
	}

	h.renderSourcesPage(c, &inventorySourcesPageData{
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
		Ok:              inventoryPageOk(c),
		Done:            inventoryPageDone(c),
		LoadFailed:      loadFailed,
		Pagination:      pagination,
	})
}

// InventorySourceCreate 新建货源（内部类型自动成为关联方）。
func (h *inventorySourcePageHandle) InventorySourceCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreateSourceReq{
		ProjectID:    projectID,
		Code:         strings.TrimSpace(c.PostForm("code")),
		Name:         strings.TrimSpace(c.PostForm("name")),
		Type:         strings.TrimSpace(c.PostForm("type")),
		Status:       strings.TrimSpace(c.PostForm("status")),
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
		Sort:         parseIntOr(c.PostForm("sort"), 0),
	}
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		redirectSourceErr(c, projectID, perr)
		return
	}
	req.SettlePrice = price
	if _, err := h.inventory.CreateSource(c.Request.Context(), req); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourceUpdate 修改货源（表单即最终状态：结算价留空 = 清空）。
func (h *inventorySourcePageHandle) InventorySourceUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	code := strings.TrimSpace(c.PostForm("code"))
	name := strings.TrimSpace(c.PostForm("name"))
	sourceType := strings.TrimSpace(c.PostForm("type"))
	status := strings.TrimSpace(c.PostForm("status"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	req := &inventorydto.UpdateSourceReq{
		ID:           c.PostForm("id"),
		Code:         &code,
		Name:         &name,
		Type:         &sourceType,
		Status:       &status,
		Sort:         &sortValue,
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
	}
	// 表单是最终状态：留空即「这条货源没有结算价」，显式交给 service 清空。
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		redirectSourceErr(c, projectID, perr)
		return
	}
	if price == nil {
		req.ClearSettlePrice = true
	} else {
		req.SettlePrice = price
	}
	if _, err := h.inventory.UpdateSource(c.Request.Context(), req); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourceDelete 删除货源。
func (h *inventorySourcePageHandle) InventorySourceDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{
		ID: c.PostForm("id"),
	}); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourcesBulkDelete 批量删除货源。
//
// 逐条走同一条删除路径：被采购单等历史数据引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带货源管理页，避免静默的部分成功。
func (h *inventorySourcePageHandle) InventorySourcesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的**受控错误**（shell.BulkIDsError：值域只有 Count/Max 两个整数，
		// 装不下表名 / 约束名 / SQLSTATE），文案走 shell 的受控出口 —— 它按当前语言拼出
		//「一次最多操作 N 项，当前 M 项，请分批进行」，不会把可行动提示抹成通用提示。
		c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
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
	target := "/admin/inventory/sources?project=" + urlQueryEscape(projectID)
	// 结论与仓库页同一口径：模板取 inventoryBulkNoticeTemplates 里那条（key + 中文兜底），
	// 经 inventoryBulkText 按当前语言取词后再 Sprintf —— 读侧 inventoryNoticeTexts 从
	// **同一张表、同一个取法**派生候选，英文页面上这条回执才不会被判成伪造。
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf(
			inventoryBulkText(c, inventoryBulkSourcePartial), strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(
			inventoryBulkText(c, inventoryBulkSourceDone), strconv.Itoa(deleted)))
	}
	c.Redirect(http.StatusFound, target)
}

// sourceSummary 关联方统计（验收 4）。统计失败不阻断页面：置空并附带提示。
// c 是给「错误文案三件套」用的（翻译 + 记日志时的 user_id）：取数失败要走 inventoryErrText，
// 而不是把 err.Error() 铺进模板。
func (h *inventorySourcePageHandle) sourceSummary(c *gin.Context, ctx context.Context, projectID string, pageErr *string) (out gin.H, ok bool) {
	out = gin.H{"Groups": []gin.H{}}
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
	groups := make([]gin.H, 0, len(res.Groups))
	for _, g := range res.Groups {
		groups = append(groups, gin.H{
			"Type": g.Type, "TypeLabel": sourceTypeLabel(g.Type),
			"RelatedParty": g.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(g.RelatedParty),
			"Count": g.Count,
		})
	}
	return gin.H{
		"Total": res.Total, "Internal": res.Internal, "External": res.External,
		"RelatedParty": res.RelatedParty, "Unrelated": res.Unrelated,
		"SettlePriced": res.SettlePriced, "Groups": groups,
	}, true
}

// sourceRow 货源行 → 模板视图（金额与配置都先格式化，模板不做逻辑）。
func sourceRow(s *inventorydto.SourceResp) gin.H {
	settlePrice := "—"
	if s.SettlePrice != nil {
		settlePrice = strconv.FormatFloat(*s.SettlePrice, 'f', 2, 64)
	}
	return gin.H{
		"ID": s.ID, "Code": s.Code, "Name": s.Name,
		"Type": s.Type, "TypeLabel": sourceTypeLabel(s.Type),
		"RelatedParty": s.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(s.RelatedParty),
		"Status": s.Status, "StatusLabel": sourceStatusLabel(s.Status),
		"SettlePrice": settlePrice, "HasSettlePrice": s.SettlePrice != nil,
		"Config": prettyJSON(s.Config), "Sort": s.Sort, "UpdatedAt": s.UpdatedAt,
	}
}

// sourceTypeOptions 类型下拉（外部 / 内部）。
func sourceTypeOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceTypeExternal, "Label": sourceTypeLabel(inventoryenums.SourceTypeExternal)},
		{"Value": inventoryenums.SourceTypeInternal, "Label": sourceTypeLabel(inventoryenums.SourceTypeInternal)},
	}
}

// sourceStatusOptions 状态下拉（启用 / 停用）。
func sourceStatusOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceStatusActive, "Label": sourceStatusLabel(inventoryenums.SourceStatusActive)},
		{"Value": inventoryenums.SourceStatusDisabled, "Label": sourceStatusLabel(inventoryenums.SourceStatusDisabled)},
	}
}

// sourceRelatedOptions 关联方三态下拉：空值 = 按类型默认（编辑时 = 不改）。
func sourceRelatedOptions() []gin.H {
	return []gin.H{
		{"Value": "", "Label": "按类型默认 / 不改"},
		{"Value": "true", "Label": sourceRelatedLabel(true)},
		{"Value": "false", "Label": sourceRelatedLabel(false)},
	}
}

// sourceTypeLabel 货源类型 → 展示文案。
func sourceTypeLabel(sourceType string) string {
	switch sourceType {
	case inventoryenums.SourceTypeInternal:
		return "内部（集团内 / 自家工厂）"
	case inventoryenums.SourceTypeExternal:
		return "外部供应商"
	default:
		return sourceType
	}
}

// sourceRelatedLabel 关联方标志 → 展示文案。
func sourceRelatedLabel(related bool) string {
	if related {
		return "关联方"
	}
	return "非关联方"
}

// sourceStatusLabel 货源状态 → 展示文案。
func sourceStatusLabel(status string) string {
	if status == inventoryenums.SourceStatusDisabled {
		return "已停用"
	}
	return "启用中"
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
		return nil, errors.New(inventoryenums.ErrSourceSettleInvalid)
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

// redirectSourceErr 回列表并把业务错误经 ?err= 回显。
//
// 走 inventoryErrURL —— 它内部会用 inventoryErrText 过一遍白名单：业务错误原样可见，
// 基础设施错误只给归口文案（原文进日志）。与库存页其余写入口是同一套助手。
func redirectSourceErr(c *gin.Context, projectID string, err error) {
	c.Redirect(http.StatusFound, inventoryErrURL(c, "/admin/inventory/sources", projectID, err))
}
