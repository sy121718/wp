package membershiphttp

// 这一层只做两件事：把运营在页面上的输入翻译成 service 的入参，把 service 的输出投影成
// **只含业务事实**的行 / 选项数据。展示口径不在这里 —— 分 → 元走模板的 money()、
// 折扣百分比走 percent()、时刻走 dateTime()、枚举文案由模板 tr() 取词：
// Go 侧不再产出任何一句界面文案，也不再拼「20%」「2026-09-26 10:00:00」这类展示串
//（同一份加工散在 Go 与模板两处时必然漂移，而且两边看起来都「正常」）。
//
// 写入由浏览器调用既有 JSON API；金额输入的单位转换由声明式写控件负责。

// 两个页面，各管一件事：
//
//	/admin/membership              —— 等级与权益：门槛、排序、默认等级、两种权益的取值
//	/admin/membership/assignments  —— 会员归属：谁是什么等级、手工指定、解除手工锁定
//
// 为什么不合成一页：等级是**配置**（改一次全站生效，改的人少、次数少），
// 归属是**日常操作**（按客户查、按等级查、手工调整，天天用）。两者挤在一页的形态
// 会让「改门槛」与「查某个人」互相干扰 —— 与库存域把「变动原因字典」从库存页拆出去
// 是同一轮设计评审的结论。
//
// GET 渲染整页，写动作直调 JSON API；错误由 membership_err.go 归口。

// 与 membership_page_handle.go 分开：那个文件是「页面怎么组装」，本文件是「一个行 / 一个选项
// 长什么样」。行视图在这里定型的好处是列表与筛选栏不会各造一份形状
// （同一个等级在两处显示成不同文案，是这类页面最常见的静默不一致）。

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/module/membership/contract"
	"go_wp/internal/module/membership/dto"
	"go_wp/internal/module/membership/enums"
	"go_wp/internal/module/membership/model"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/utils"
)

// formInt 解析整数输入，空串或非法值回落默认值。
func formInt(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// formInt64 解析 64 位整数输入，空串或非法值回落默认值。
func formInt64(raw string, fallback int64) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

const (
	membershipTiersPath       = "/admin/membership"
	membershipAssignmentsPath = "/admin/membership/assignments"
)

// membershipPageHandle 后台页面处理器。
type membershipPageHandle struct {
	svc      membershipcontract.MembershipService
	projects projectcontract.ProjectService
}

// NewMembershipPageHandle 构造。
func NewMembershipPageHandle(svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) *membershipPageHandle {
	return &membershipPageHandle{svc: svc, projects: projects}
}

// membershipTierRow 等级列表的一行 —— **只装业务事实**：门槛保持「分」、折扣保持原始数字、
// 权益的存在与取值分开存。展示口径（分 → 元、百分比串、文案）全部留给模板。
type membershipTierRow struct {
	ID        int64
	Name      string
	SortOrder int
	// ThresholdAmount 升级门槛，单位**分**（与 orders 的金额列同口径）。
	ThresholdAmount int64
	IsDefault       bool
	Remark          string
	// FreeShipping 是否配了免运费权益（value_int == 1）。
	FreeShipping bool
	// HasDiscount 是否配了折扣权益；DiscountPercent 是它的取值（1..100，20 = 打八折）。
	//
	// 「有没有这条权益」与「取值是多少」分开：编辑表单要靠前者决定折扣 input 留空还是回填数字
	//（取值域是 1..100，回填一个 0 只会被当成越界）。
	HasDiscount     bool
	DiscountPercent int64
	// HasEntitlement 该等级是否配了至少一种权益（空态文案与「两种都是关」要能分辨）。
	HasEntitlement bool
}

// membershipTierEdit 等级编辑区的回填数据（?edit=<id> 时渲染）。
//
// 用「服务端按 query 参数渲染编辑区」而不是抽屉 + JS 回填：本仓不新增散落业务 JS
// （AGENTS.md §交互方式），而编辑区要回填的值（门槛的元表示、折扣的原始数字）
// 从 HTML 属性里读出来再塞进 input 是一段没人维护的胶水代码。
type membershipTierEdit struct {
	ID              int64
	Name            string
	SortOrder       int
	ThresholdAmount int64
	Remark          string
	FreeShipping    bool
	HasDiscount     bool
	DiscountPercent int64
	IsDefault       bool
}

// membershipAssignRow 归属列表的一行 —— 只装业务事实。
//
// Source 给的是**原始枚举值**（auto / manual）：来源的显示名、以及「这一行能不能解除锁定」，
// 都由模板按它判（取值从 .SourceManual / .SourceAuto 拿，模板里不硬编码字面量）。
// AssignedAt 给原始时刻，格式化在模板（dateTime）。
type membershipAssignRow struct {
	ID         int64
	UserID     uint64
	TierID     int64
	TierName   string
	Source     string
	AssignedAt utils.JSONTime
}

// —— 等级与权益页 ——

// membershipTiersPageData 等级页的模板数据（正常渲染与降级渲染共用一份拼装）。
//
// 单独一个结构体的理由：降级分支若另抄一份 gin.H，两处的键集必然分叉，
// 而模板缺 key 的后果是 renderError → HTTP 500（buffer 里的半截内容被丢弃）。
type membershipTiersPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Tiers           []membershipTierRow
	// Edit 当前正在编辑的等级（nil = 不显示编辑区）。
	Edit *membershipTierEdit
	// KindFreeShippingValue / KindDiscountValue 权益类型的表单取值（模板不用硬编码字符串）。
	KindFreeShippingValue string
	KindDiscountValue     string
	// ListQuery 列表上下文（工程）的查询串，拼进写动作表单的 action。
	ListQuery string
	// LoadErr 这一次列表没读出来的原因（空串 = 正常）。
	LoadErr string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与货源页同名的判据：模板里没有可靠办法分辨「Tiers 为空」是「这个工程真的没有等级」
	// 还是「这一次没读出来」—— 前者要引导去建一个，后者只能说「稍后重试」。
	LoadFailed bool
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
//
// 这里**不注入任何界面文案**：两张页面的枚举显示名（来源、权益类型）都由模板 tr() 取词，
// 与列表列共用同一份 key —— Go 侧再翻一遍必然出现「同一个值在两处显示成两个词」。
func (d *membershipTiersPageData) templateMap(c *gin.Context) gin.H {
	return gin.H{
		"title":                 d.Title,
		"menu":                  d.Menu,
		"Projects":              d.Projects,
		"SelectedProject":       d.SelectedProject,
		"Tiers":                 d.Tiers,
		"Edit":                  d.Edit,
		"HasTiers":              len(d.Tiers) > 0,
		"KindFreeShippingValue": d.KindFreeShippingValue,
		"KindDiscountValue":     d.KindDiscountValue,
		"ListQuery":             d.ListQuery,
		"LoadErr":               d.LoadErr,
		"LoadFailed":            d.LoadFailed,
	}
}

// renderTiersPage 等级页的唯一渲染出口（正常与降级两条路都从这里出）。
func (h *membershipPageHandle) renderTiersPage(c *gin.Context, d *membershipTiersPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership.html", adminui.Prepare(c, d.templateMap(c)))
}

// MembershipPage 等级与权益页。
func (h *membershipPageHandle) MembershipPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 列表取数失败的提示（空串 = 正常）；写结果由 JSON API 返回。
	loadErrText := ""

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		loadErrText = membershipErrPageText(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = h.pickProject(c.Query("project"), projects)
	}

	rows := []membershipTierRow{}
	var edit *membershipTierEdit
	if !loadFailed && selected != "" {
		list, lerr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if lerr != nil {
			if loadErrText == "" {
				loadErrText = membershipErrPageText(c, lerr)
			}
		} else {
			for _, tier := range list {
				rows = append(rows, membershipTierRowOf(tier))
			}
			// ?edit=<id> 指向本工程的某个等级时渲染编辑区；指向不存在 / 别的工程的 id
			// 时**静默不显示**（列表里那个 id 本来就不该被点开，报错反而把「别人贴了个
			// 过期链接」变成一次可见的故障）。
			edit = membershipTierEditOf(list, formInt64(c.Query("edit"), 0))
		}
	}

	h.renderTiersPage(c, &membershipTiersPageData{
		Title:                 membershipenums.PageTiersTitle,
		Menu:                  "membership",
		Projects:              projects,
		SelectedProject:       selected,
		Tiers:                 rows,
		Edit:                  edit,
		KindFreeShippingValue: membershipenums.KindFreeShipping,
		KindDiscountValue:     membershipenums.KindDiscount,
		ListQuery:             membershipListQuery(map[string]string{"project": selected}),
		LoadErr:               loadErrText,
		LoadFailed:            loadFailed,
	})
}

// —— 归属页 ——

// membershipAssignmentsPageData 归属页的模板数据。
type membershipAssignmentsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	TierOptions     []gin.H
	SourceOptions   []gin.H
	FilterTier      int64
	FilterSource    string
	FilterUser      uint64
	Rows            []membershipAssignRow
	Total           int64
	// ListQuery 列表上下文（工程 / 等级 / 来源 / 访客 / 页码）的查询串，拼进写动作表单的 action。
	ListQuery string
	// LoadErr 这一次列表没读出来的原因（空串 = 正常）。
	LoadErr    string
	LoadFailed bool
	// RecalcAvailable 消费额批量端口是否已接入（决定页面上「立即重算」按钮是否可用）。
	//
	// 显示而不是隐藏：运营需要知道「这个功能还没接线」，而不是点了按钮拿到一句
	// 「重算不可用」的报错 —— 后者会被当成故障上报。
	RecalcAvailable bool
	Pagination      pagination.Page
}

// templateMap 转 Jet 模板键（来源文案同样不在这里取词，见 membershipTiersPageData.templateMap）。
func (d *membershipAssignmentsPageData) templateMap(c *gin.Context) gin.H {
	m := gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"TierOptions":     d.TierOptions,
		"SourceOptions":   d.SourceOptions,
		"FilterTier":      d.FilterTier,
		"FilterSource":    d.FilterSource,
		"FilterUser":      d.FilterUser,
		"Rows":            d.Rows,
		"Total":           d.Total,
		"ListQuery":       d.ListQuery,
		"LoadErr":         d.LoadErr,
		"LoadFailed":      d.LoadFailed,
		"RecalcAvailable": d.RecalcAvailable,
		// 来源的**取值**（不是文案）：模板按它判这一行来自自动重算还是手工指定
		//（显示名与「能不能解除锁定」都由模板决定）。
		"SourceManual": membershipmodel.SourceManual,
		"SourceAuto":   membershipmodel.SourceAuto,
	}
	// 分页**事实**（pagination.Page，值语义）：零值就是「没有分页」，单页与装载失败
	// 两条路都让 partial 的 {{if .Pagination.HasPagination}} 为假，自然不渲染分页条。
	// 文案、链接与选中态由 admin/partials/pagination.html 用通用模板函数渲染。
	m["Pagination"] = d.Pagination
	return m
}

// renderAssignmentsPage 归属页的唯一渲染出口。
func (h *membershipPageHandle) renderAssignmentsPage(c *gin.Context, d *membershipAssignmentsPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership_assignments.html", adminui.Prepare(c, d.templateMap(c)))
}

// MembershipAssignmentsPage 会员归属页。
func (h *membershipPageHandle) MembershipAssignmentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 列表取数失败的提示（空串 = 正常）；写结果由 JSON API 返回。
	loadErrText := ""

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		loadErrText = membershipErrPageText(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = h.pickProject(c.Query("project"), projects)
	}

	filterTier := formInt64(c.Query("tierId"), 0)
	filterSource := c.Query("source")
	filterUser := uint64(formInt64(c.Query("userId"), 0))
	page, limit := normalizePageParams(c)

	rows := []membershipAssignRow{}
	tierOptions := []gin.H{}
	var total int64
	var pg pagination.Page

	if !loadFailed && selected != "" {
		// 等级下拉与行上的等级名都来自这一份清单：一次查询同时支撑筛选栏与列表渲染。
		tiers, terr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if terr != nil {
			if loadErrText == "" {
				loadErrText = membershipErrPageText(c, terr)
			}
		} else {
			tierOptions = membershipTierOptions(tiers, filterTier)
		}

		n, cerr := h.svc.CountAssignments(ctx, &membershipdto.CountAssignmentsReq{
			ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
		})
		if cerr != nil {
			if loadErrText == "" {
				loadErrText = membershipErrPageText(c, cerr)
			}
		} else {
			total = n
			page = clampPage(page, limit, total)
			list, lerr := h.svc.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
				ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
				Page: page, Size: limit,
			})
			if lerr != nil {
				if loadErrText == "" {
					loadErrText = membershipErrPageText(c, lerr)
				}
			} else {
				for _, item := range list {
					rows = append(rows, membershipAssignRowOf(item))
				}
			}
			if p := pagination.Facts(total, page, limit); p.HasPagination() {
				p.Base = reqparams.FilterBaseURL(membershipAssignmentsPath, map[string]string{
					"project": selected,
					"tierId":  itoa64(filterTier),
					"source":  filterSource,
					"userId":  itoaU64(filterUser),
				})
				pg = p
			}
		}
	}

	h.renderAssignmentsPage(c, &membershipAssignmentsPageData{
		Title:           membershipenums.PageAssignmentsTitle,
		Menu:            "membership-assignments",
		Projects:        projects,
		SelectedProject: selected,
		TierOptions:     tierOptions,
		SourceOptions:   membershipSourceOptions(filterSource),
		FilterTier:      filterTier,
		FilterSource:    filterSource,
		FilterUser:      filterUser,
		Rows:            rows,
		Total:           total,
		ListQuery: membershipListQuery(map[string]string{
			"project": selected,
			"tierId":  itoa64(filterTier),
			"source":  filterSource,
			"userId":  itoaU64(filterUser),
			"page":    strconv.Itoa(page),
		}),
		LoadErr:         loadErrText,
		LoadFailed:      loadFailed,
		RecalcAvailable: h.recalcAvailable(),
		Pagination:      pg,
	})
}

// —— 内部助手 ——

// listProjects 读工程清单（projects 未注入时返回空清单而不是错误 —— 页面仍可渲染）。
func (h *membershipPageHandle) listProjects(ctx context.Context) ([]projectcontract.ProjectResp, error) {
	if h.projects == nil {
		return nil, nil
	}
	return h.projects.List(ctx)
}

// pickProject 定出当前工程：URL 指定优先，否则取第一个。
func (h *membershipPageHandle) pickProject(raw string, projects []projectcontract.ProjectResp) string {
	if v := trimSpace(raw); v != "" {
		return v
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// recalcAvailable 消费额批量端口是否已接入（未接入时页面上的「立即重算」按钮置灰并说明）。
func (h *membershipPageHandle) recalcAvailable() bool {
	if h.svc == nil {
		return false
	}
	// 用一次空工程的 RecalcProject 试是不行的（它有副作用）。
	// 端口是否注入只有实现知道，因此经一个**只读**的窄接口问它 —— 见 recalcProbe。
	if probe, ok := h.svc.(recalcProbe); ok {
		return probe.RecalcReady()
	}
	return false
}

// recalcProbe 判定「归属重算是否可用」的窄接口（由 service 实现）。
//
// 为什么不直接判端口类型：handler 拿不到 service 的私有字段，
// 而在 handler 里重新拿一次端口等于维护第二份状态来源。
type recalcProbe interface {
	RecalcReady() bool
}

// 归属页的分页参数（与 service 的 maxPageSize 同口径：超过上限即截，而不是让请求方决定查询规模）。
const (
	assignmentsPageSize    = 20
	assignmentsMaxPageSize = 200
)

// membershipListQuery 列表上下文的查询串（拼进写动作表单的 action）。
//
// 空值丢弃、编码走 url.Values（键有序、产物稳定）—— 与 backpath.WithParams 同口径。
// 只带筛选上下文，供 JSON 写成功导航回列表。
func membershipListQuery(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	return q.Encode()
}

// membershipTierRowOf 等级响应 → 列表行视图。
//
// 只搬运事实：门槛保持「分」、折扣保持原始数字 —— 换算与文案都在模板里做
// （模板那边有 money / percent / tr，同一个页面不会出现两份口径）。
func membershipTierRowOf(tier *membershipdto.TierResp) membershipTierRow {
	row := membershipTierRow{
		ID:              tier.ID,
		Name:            tier.Name,
		SortOrder:       tier.SortOrder,
		ThresholdAmount: tier.ThresholdAmount,
		IsDefault:       tier.IsDefault,
		Remark:          tier.Remark,
	}
	for _, ent := range tier.Entitlements {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			row.FreeShipping = ent.ValueInt == 1
			row.HasEntitlement = true
		case membershipenums.KindDiscount:
			row.HasDiscount = true
			row.DiscountPercent = ent.ValueInt
			row.HasEntitlement = true
		}
	}
	return row
}

// membershipAssignRowOf 归属响应 → 列表行视图。
//
// 纯函数、不碰请求上下文：来源只搬原始枚举值、时刻只搬原始时间。
// 这样「同一行在什么语言下渲染成什么」完全由模板决定，行视图本身可单测
// （旧实现把 tr 传进来取词，行视图就没法脱离请求上下文断言）。
func membershipAssignRowOf(item *membershipdto.AssignmentResp) membershipAssignRow {
	return membershipAssignRow{
		ID:         item.ID,
		UserID:     item.UserID,
		TierID:     item.TierID,
		TierName:   item.TierName,
		Source:     item.Source,
		AssignedAt: item.AssignedAt,
	}
}

// membershipSourceOptions 来源筛选下拉的**取值**（空值 = 全部）。
//
// 只给两件事实：「有哪些取值」与「当前选中哪一个」。选项的显示名是 UI 文案，
// 由模板 tr() 取词 —— Go 侧不再产出任何一句界面文案。
//
// Selected 仍由服务端算好：让模板比较「选项值 == 根级过滤器」要引入根引用（$），
// 而「哪个选项被选中」是一条纯数据判断，放在这里零风险。
func membershipSourceOptions(selected string) []gin.H {
	values := []string{"", membershipmodel.SourceAuto, membershipmodel.SourceManual}
	out := make([]gin.H, 0, len(values))
	for _, v := range values {
		out = append(out, gin.H{"Value": v, "Selected": v == selected})
	}
	return out
}

// membershipTierOptions 等级筛选 / 选择下拉。
//
// 只列**真实存在的等级**（Label 是等级名 —— 那是运营输入的数据，不是 UI 文案）。
// 「全部」那一项由模板自己渲染：筛选栏必须有它、手工指定表单绝不能有它
// （选中「全部」等于提交 tierId=0，service 只能回一句「等级不存在」）。
//
// Selected 由服务端算好（判据同 membershipSourceOptions）。
func membershipTierOptions(tiers []*membershipdto.TierResp, selected int64) []gin.H {
	out := make([]gin.H, 0, len(tiers))
	for _, tier := range tiers {
		out = append(out, gin.H{"Value": tier.ID, "Label": tier.Name, "Selected": tier.ID == selected})
	}
	return out
}

// normalizePageParams 读分页参数（缺省每页 20，上限 200）。
func normalizePageParams(c *gin.Context) (page, limit int) {
	page = formInt(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	limit = formInt(c.Query("limit"), assignmentsPageSize)
	if limit < 1 {
		limit = assignmentsPageSize
	}
	if limit > assignmentsMaxPageSize {
		limit = assignmentsMaxPageSize
	}
	return page, limit
}

// clampPage 把页码收敛到有效范围（先计数、再收敛、最后取当页 —— 顺序不能反：
// 先取数再收敛会让「页码越界」表现为空列表而不是最后一页）。
func clampPage(page, limit int, total int64) int {
	if limit <= 0 {
		return 1
	}
	maxPage := int((total + int64(limit) - 1) / int64(limit))
	if maxPage < 1 {
		return 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

// itoa64 / itoaU64 拼分页链接用（0 值与空串在 URL 上都表示「不筛」，两处保持同一形态）。
func itoa64(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

// itoaU64 同上。
func itoaU64(v uint64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(v, 10)
}

// trimSpace 去首尾空白（工程 id 从 query / 表单进来）。
func trimSpace(raw string) string { return strings.TrimSpace(raw) }

// membershipTierEditOf 从等级清单里取出待编辑的那一条（id 为 0 或查不到时返回 nil）。
//
// 查的是**同一个工程的清单**（而不是再查一次库）：清单已经在手上，
// 而且「编辑区里的值」与「列表里显示的值」因此必然同源 —— 两处各查一次时，
// 并发编辑下会出现「列表显示旧值、编辑框显示新值」。
func membershipTierEditOf(list []*membershipdto.TierResp, tierID int64) *membershipTierEdit {
	if tierID <= 0 {
		return nil
	}
	for _, tier := range list {
		if tier.ID != tierID {
			continue
		}
		edit := &membershipTierEdit{
			ID:              tier.ID,
			Name:            tier.Name,
			SortOrder:       tier.SortOrder,
			ThresholdAmount: tier.ThresholdAmount,
			Remark:          tier.Remark,
			IsDefault:       tier.IsDefault,
		}
		for _, ent := range tier.Entitlements {
			switch ent.Kind {
			case membershipenums.KindFreeShipping:
				edit.FreeShipping = ent.ValueInt == 1
			case membershipenums.KindDiscount:
				edit.HasDiscount = true
				edit.DiscountPercent = ent.ValueInt
			}
		}
		return edit
	}
	return nil
}
