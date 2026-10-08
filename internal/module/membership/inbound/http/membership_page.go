package membershiphttp

// 这一层只做两件事：把运营在页面上的输入翻译成 service 的入参，把 service 的输出翻译成
// 模板直接可渲染的文本。**换算只在这里做一次** —— 门槛在库里是「分」（与 orders 的金额列同口径），
// 而运营的输入与阅读习惯是「元」：这个边界就是本文件，别处不许再有第二次换算
// （两次换算的典型症状是「填 1000 存成 100000」，而两处看起来都合理）。

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
// 页面形态照本仓既有后台页：GET 渲染整页、POST 写动作的结论由 shell.RenderJump 渲染成
// 整页提示（文案走响应体，不再经 ?err= / ?done= 回带），数据由 templateMap 组装
// （模板只渲染、不查询）。错误一律经 membershipErr.go 的三件套。

// 与 membership_page_handle.go 分开：那个文件是「页面怎么组装」，本文件是「一个行 / 一个选项
// 长什么样」。行视图在这里定型的好处是列表与筛选栏不会各造一份形状
// （同一个等级在两处显示成不同文案，是这类页面最常见的静默不一致）。

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/membership/contract"
	"go_wp/internal/module/membership/dto"
	"go_wp/internal/module/membership/enums"
	"go_wp/internal/module/membership/model"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

// parseYuanToFen 把「元」输入解析成「分」（整数运算，不经过浮点）。
//
// 不用 strconv.ParseFloat(raw, 64) * 100：0.1 这类二进制无法精确表示的值乘 100 会得到
// 9.999999999999998，四舍五入之后偶尔差 1 分 —— 而门槛差 1 分会把「消费满 1000 元」的会员
// 挡在门外一单。整数拆分（整数部分 + 两位小数）没有这个问题。
//
// 接受形态：`1000` / `1000.5` / `1000.50` / `1,000.50`（千分位容忍，运营从表格里复制常见）；
// 空串返回 (0, nil) —— 调用方据此区分「没填」与「填了 0」。
func parseYuanToFen(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	value = strings.ReplaceAll(value, ",", "")
	if value == "" {
		return 0, nil
	}
	neg := false
	if strings.HasPrefix(value, "-") {
		neg = true
		value = value[1:]
	}
	intPart, fracPart, _ := strings.Cut(value, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 2 {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	// 一位小数补零到两位，保证「1.5 元」读作 150 分而不是 15 分。
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	if fracPart == "" {
		fracPart = "00"
	}
	whole, ierr := strconv.ParseInt(intPart, 10, 64)
	if ierr != nil {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	frac, ferr := strconv.ParseInt(fracPart, 10, 64)
	if ferr != nil {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	fen := whole*100 + frac
	if neg {
		fen = -fen
	}
	return fen, nil
}

// formatFenToYuan 把「分」格式化成「元」的两位小数字符串（表单回填与列表展示共用）。
func formatFenToYuan(fen int64) string {
	neg := fen < 0
	if neg {
		fen = -fen
	}
	out := strconv.FormatInt(fen/100, 10) + "." + twoDigits(fen%100)
	if neg {
		return "-" + out
	}
	return out
}

// twoDigits 补足两位（0 → "00"，5 → "05"）。
func twoDigits(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// formBool 解析表单里的勾选值（HTML checkbox 未勾选时字段根本不存在，值为空串）。
func formBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

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

// parseEntitlementsFromForm 从等级表单里抽出权益清单（新建等级与保存权益共用）。
//
// 表单形态是「两个固定字段」而不是动态行：本期只交付两种权益
// （免运费 / 折扣），固定字段让「没填折扣」与「折扣填 0」能被区分 ——
// 前者是不设折扣，后者（0%）在取值域之外（折扣只接受 1..100）。
func parseEntitlementsFromForm(c *gin.Context) []membershipentitlementForm {
	items := make([]membershipentitlementForm, 0, 2)
	items = append(items, membershipentitlementForm{
		Enabled:  c.PostForm("freeShipping") != "",
		Kind:     membershipenums.KindFreeShipping,
		ValueInt: boolToInt64(formBool(c.PostForm("freeShipping"))),
	})
	discountRaw := strings.TrimSpace(c.PostForm("discountPercent"))
	items = append(items, membershipentitlementForm{
		Enabled:  discountRaw != "",
		Kind:     membershipenums.KindDiscount,
		ValueInt: formInt64(discountRaw, 0),
	})
	return items
}

// membershipentitlementForm 表单里一条权益的原始输入（Enabled = 运营在页面上给了值）。
type membershipentitlementForm struct {
	Enabled  bool
	Kind     string
	ValueInt int64
}

// boolToInt64 布尔 → 免运费的 value_int（0/1）。
func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// 页面路径常量（回跳与分页基址都用同一份字面量，避免两处各写一遍而慢慢漂移）。
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

// membershipTierRow 等级列表的一行（模板直接渲染，不在模板里做换算或判断）。
type membershipTierRow struct {
	ID            int64
	Name          string
	SortOrder     int
	ThresholdYuan string
	IsDefault     bool
	Remark        string
	FreeShipping  bool
	DiscountText  string
	// DiscountRaw 折扣的原始数字（编辑表单回填 input 用；展示用 DiscountText）。
	DiscountRaw string
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
	ThresholdYuan   string
	Remark          string
	FreeShipping    bool
	DiscountPercent string
	IsDefault       bool
}

// membershipAssignRow 归属列表的一行。
type membershipAssignRow struct {
	ID         int64
	UserID     uint64
	TierID     int64
	TierName   string
	Source     string
	SourceText string
	IsManual   bool
	AssignedAt string
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
	// 服务端自己拼：页面不把上下文塞进隐藏域，回跳时由 shell.BackPath 按白名单读回来。
	ListQuery string
	// LoadErr 这一次列表没读出来的原因（空串 = 正常）。
	// 写动作的结论不在这里：它由提示页在响应体里渲染（shell.RenderJump）。
	LoadErr string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与货源页同名的判据：模板里没有可靠办法分辨「Tiers 为空」是「这个工程真的没有等级」
	// 还是「这一次没读出来」—— 前者要引导去建一个，后者只能说「稍后重试」。
	LoadFailed bool
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
func (d *membershipTiersPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
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
		"KindFreeShippingLabel": tr(membershipenums.KindLabelFreeShipping, "免运费"),
		"KindDiscountLabel":     tr(membershipenums.KindLabelDiscount, "折扣"),
		"ListQuery":             d.ListQuery,
		"LoadErr":               d.LoadErr,
		"LoadFailed":            d.LoadFailed,
	}
}

// renderTiersPage 等级页的唯一渲染出口（正常与降级两条路都从这里出）。
func (h *membershipPageHandle) renderTiersPage(c *gin.Context, d *membershipTiersPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership.html", shell.Prepare(c, d.templateMap(c)))
}

// MembershipPage 等级与权益页。
func (h *membershipPageHandle) MembershipPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 列表取数失败的提示（空串 = 正常）。写动作的结论不在这里：它由提示页在响应体里
	// 渲染（shell.RenderJump），读侧 ?err= 已整批删除。
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
	Pagination      *shell.PaginationData
}

// templateMap 转 Jet 模板键。
func (d *membershipAssignmentsPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
	m := gin.H{
		"title":            d.Title,
		"menu":             d.Menu,
		"Projects":         d.Projects,
		"SelectedProject":  d.SelectedProject,
		"TierOptions":      d.TierOptions,
		"SourceOptions":    d.SourceOptions,
		"FilterTier":       d.FilterTier,
		"FilterSource":     d.FilterSource,
		"FilterUser":       d.FilterUser,
		"Rows":             d.Rows,
		"Total":            d.Total,
		"ListQuery":        d.ListQuery,
		"LoadErr":          d.LoadErr,
		"LoadFailed":       d.LoadFailed,
		"RecalcAvailable":  d.RecalcAvailable,
		"SourceManual":     membershipmodel.SourceManual,
		"SourceAuto":       membershipmodel.SourceAuto,
		"SourceManualText": tr(membershipenums.SourceLabelManual, "手工指定"),
		"SourceAutoText":   tr(membershipenums.SourceLabelAuto, "自动重算"),
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderAssignmentsPage 归属页的唯一渲染出口。
func (h *membershipPageHandle) renderAssignmentsPage(c *gin.Context, d *membershipAssignmentsPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership_assignments.html", shell.Prepare(c, d.templateMap(c)))
}

// MembershipAssignmentsPage 会员归属页。
func (h *membershipPageHandle) MembershipAssignmentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 列表取数失败的提示（空串 = 正常）。写动作的结论不在这里：它由提示页在响应体里
	// 渲染（shell.RenderJump），读侧 ?err= 已整批删除。
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
	var pagination *shell.PaginationData

	if !loadFailed && selected != "" {
		// 等级下拉与行上的等级名都来自这一份清单：一次查询同时支撑筛选栏与列表渲染。
		tiers, terr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if terr != nil {
			if loadErrText == "" {
				loadErrText = membershipErrPageText(c, terr)
			}
		} else {
			tierOptions = membershipTierOptions(c, tiers, filterTier)
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
					rows = append(rows, membershipAssignRowOf(shell.TranslateFor(c), item))
				}
			}
			pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
				membershipAssignmentsPath, map[string]string{
					"project": selected,
					"tierId":  itoa64(filterTier),
					"source":  filterSource,
					"userId":  itoaU64(filterUser),
				}), shell.TranslateFor(c))
		}
	}

	h.renderAssignmentsPage(c, &membershipAssignmentsPageData{
		Title:           membershipenums.PageAssignmentsTitle,
		Menu:            "membership-assignments",
		Projects:        projects,
		SelectedProject: selected,
		TierOptions:     tierOptions,
		SourceOptions:   membershipSourceOptions(c, filterSource),
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
		Pagination:      pagination,
	})
}

// —— 写动作（表单 POST → shell.RenderJump 渲染提示页）——
//
// 每个动作都只做「解析表单 → 调 service → 出口渲染」，业务判据全在 service：
// 页面这一层不该有第二份「谁能改什么」的规则（两份必然漂移，且漂移方向不可预测）。
//
// 回跳地址由 shell.BackPath 从**表单 action 的 query**（页面渲染时拼进去的筛选上下文）
// 读回，服务端自己拼 —— 不再把上下文塞进隐藏域、也不再把结论文案拼进 URL。

// TierCreate 新建等级（可同时带权益）。
func (h *membershipPageHandle) TierCreate(c *gin.Context) {
	back := shell.BackPath(c, membershipTiersPath, membershipTierBackKeys...)
	backText := membershipTierBackText(c)
	projectID := c.PostForm("projectId")
	thresholdFen, aerr := parseYuanToFen(c.PostForm("thresholdYuan"))
	if aerr != nil {
		membershipJump(c, false, membershipErrPageText(c, aerr), back, backText)
		return
	}
	req := &membershipdto.CreateTierReq{
		ProjectID:       projectID,
		Name:            c.PostForm("name"),
		SortOrder:       formInt(c.PostForm("sortOrder"), 0),
		ThresholdAmount: thresholdFen,
		IsDefault:       formBool(c.PostForm("isDefault")),
		Remark:          c.PostForm("remark"),
		Entitlements:    entitlementsFromForm(c),
	}
	if _, err := h.svc.CreateTier(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgTierCreated), back, backText)
}

// TierUpdate 更新等级（表单**不含** is_default —— 那是独立的「设为默认」动作）。
func (h *membershipPageHandle) TierUpdate(c *gin.Context) {
	back := shell.BackPath(c, membershipTiersPath, membershipTierBackKeys...)
	backText := membershipTierBackText(c)
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	thresholdFen, aerr := parseYuanToFen(c.PostForm("thresholdYuan"))
	if aerr != nil {
		membershipJump(c, false, membershipErrPageText(c, aerr), back, backText)
		return
	}
	name := c.PostForm("name")
	remark := c.PostForm("remark")
	sortOrder := formInt(c.PostForm("sortOrder"), 0)
	req := &membershipdto.UpdateTierReq{
		ProjectID:       projectID,
		TierID:          tierID,
		Name:            &name,
		SortOrder:       &sortOrder,
		ThresholdAmount: &thresholdFen,
		Remark:          &remark,
	}
	if _, err := h.svc.UpdateTier(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgTierUpdated), back, backText)
}

// TierDefault 把某个等级设为默认等级（原默认自动让位）。
//
// 独立动作而不是编辑表单里的一个复选框：复选框的「未勾选」会与「取消当前默认等级」
// 撞在一起（后者必须被拒绝），于是编辑备注这类无关操作会因为没勾那个框而失败。
// 独立成动作后语义是单向的（「设为默认」），不存在被误触发的取消路径。
func (h *membershipPageHandle) TierDefault(c *gin.Context) {
	back := shell.BackPath(c, membershipTiersPath, membershipTierBackKeys...)
	backText := membershipTierBackText(c)
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	yes := true
	req := &membershipdto.UpdateTierReq{ProjectID: projectID, TierID: tierID, IsDefault: &yes}
	if _, err := h.svc.UpdateTier(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgTierUpdated), back, backText)
}

// TierDelete 删除等级。
func (h *membershipPageHandle) TierDelete(c *gin.Context) {
	back := shell.BackPath(c, membershipTiersPath, membershipTierBackKeys...)
	backText := membershipTierBackText(c)
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	if err := h.svc.DeleteTier(c.Request.Context(), &membershipdto.DeleteTierReq{
		ProjectID: projectID, TierID: tierID,
	}); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgTierDeleted), back, backText)
}

// EntitlementSave 全量保存某等级的权益（表单即最终状态）。
func (h *membershipPageHandle) EntitlementSave(c *gin.Context) {
	back := shell.BackPath(c, membershipTiersPath, membershipTierBackKeys...)
	backText := membershipTierBackText(c)
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("tierId"), 0)
	req := &membershipdto.SaveEntitlementsReq{
		ProjectID: projectID,
		TierID:    tierID,
		Items:     entitlementsFromForm(c),
	}
	if _, err := h.svc.SaveEntitlements(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgEntitlementSaved), back, backText)
}

// AssignSet 手工指定某访客的等级。
func (h *membershipPageHandle) AssignSet(c *gin.Context) {
	back := shell.BackPath(c, membershipAssignmentsPath, membershipAssignBackKeys...)
	backText := membershipAssignBackText(c)
	projectID := c.PostForm("projectId")
	req := &membershipdto.AssignManualReq{
		ProjectID: projectID,
		UserID:    uint64(formInt64(c.PostForm("userId"), 0)),
		TierID:    formInt64(c.PostForm("tierId"), 0),
	}
	if _, err := h.svc.AssignManual(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgAssignSet), back, backText)
}

// AssignUnlock 取消手工锁定。
func (h *membershipPageHandle) AssignUnlock(c *gin.Context) {
	back := shell.BackPath(c, membershipAssignmentsPath, membershipAssignBackKeys...)
	backText := membershipAssignBackText(c)
	projectID := c.PostForm("projectId")
	req := &membershipdto.UnlockManualReq{
		ProjectID: projectID,
		UserID:    uint64(formInt64(c.PostForm("userId"), 0)),
	}
	if err := h.svc.UnlockManual(c.Request.Context(), req); err != nil {
		membershipJump(c, false, membershipErrPageText(c, err), back, backText)
		return
	}
	membershipJump(c, true, membershipNotice(c, membershipenums.MsgAssignUnlocked), back, backText)
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

// membershipNoticeFallback 成功回执的中文兜底（词条缺失时显示它，而不是裸 key）。
//
// 与迁移 462a 的 seed 值逐条一致：兜底与词条不一致时，英文界面上会突然冒出一句中文，
// 而两边看起来都「有值」。
var membershipNoticeFallback = map[string]string{
	membershipenums.MsgTierCreated:      "等级已创建",
	membershipenums.MsgTierUpdated:      "等级已更新",
	membershipenums.MsgTierDeleted:      "等级已删除",
	membershipenums.MsgEntitlementSaved: "权益已保存",
	membershipenums.MsgAssignSet:        "已指定会员等级",
	membershipenums.MsgAssignUnlocked:   "已取消手工锁定（下次日结按消费额重算）",
}

// membershipNotice 取一条成功回执的当前语言文字（渲染进提示页，见 membershipJump）。
func membershipNotice(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, membershipNoticeFallback[key])
}

// membershipListQuery 列表上下文的查询串（拼进写动作表单的 action）。
//
// 空值丢弃、编码走 url.Values（键有序、产物稳定）—— 与 shell.WithParams 同口径。
// 只带**筛选上下文**，不带任何结论文案 —— 结论走响应体（见 shell.RenderJump）。
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
// 换算与拼接都在这里做完：模板只渲染，不做判断（Jet 里做逻辑的代价是出错时整页 500）。
func membershipTierRowOf(tier *membershipdto.TierResp) membershipTierRow {
	row := membershipTierRow{
		ID:            tier.ID,
		Name:          tier.Name,
		SortOrder:     tier.SortOrder,
		ThresholdYuan: formatFenToYuan(tier.ThresholdAmount),
		IsDefault:     tier.IsDefault,
		Remark:        tier.Remark,
	}
	for _, ent := range tier.Entitlements {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			row.FreeShipping = ent.ValueInt == 1
			row.HasEntitlement = true
		case membershipenums.KindDiscount:
			row.DiscountText = strconv.FormatInt(ent.ValueInt, 10) + "%"
			row.DiscountRaw = strconv.FormatInt(ent.ValueInt, 10)
			row.HasEntitlement = true
		}
	}
	return row
}

// membershipAssignRowOf 归属响应 → 列表行视图。
//
// tr 由调用点传入（不在这里现取）：行视图是纯函数，取词依赖请求上下文，
// 混在一起会让「同一行在不同语言下渲染成什么」变得不可单测。
func membershipAssignRowOf(tr func(key, fallback string) string, item *membershipdto.AssignmentResp) membershipAssignRow {
	key, fallback := membershipSourceLabelKey(item.Source)
	return membershipAssignRow{
		ID:         item.ID,
		UserID:     item.UserID,
		TierID:     item.TierID,
		TierName:   item.TierName,
		Source:     item.Source,
		SourceText: tr(key, fallback),
		IsManual:   item.Source == membershipmodel.SourceManual,
		AssignedAt: item.AssignedAt.Time().Format("2006-01-02 15:04:05"),
	}
}

// membershipSourceOptions 来源筛选下拉（空值 = 全部）。
//
// Selected 由服务端算好而不是让模板比较：Jet 的 if 只接受 bool，
// 在模板里写 `{{if s.Value == $.FilterSource}}` 既有根引用（$）的解析风险，
// 也把「哪个选项被选中」这条判断摊到模板里 —— 出错时整页 500。
func membershipSourceOptions(c *gin.Context, selected string) []gin.H {
	tr := shell.TranslateFor(c)
	options := []struct{ Value, Label string }{
		{"", tr("admin.common.filter.optionAll", "全部")},
		{membershipmodel.SourceAuto, tr(membershipenums.SourceLabelAuto, "自动重算")},
		{membershipmodel.SourceManual, tr(membershipenums.SourceLabelManual, "手工指定")},
	}
	out := make([]gin.H, 0, len(options))
	for _, opt := range options {
		out = append(out, gin.H{"Value": opt.Value, "Label": opt.Label, "Selected": opt.Value == selected})
	}
	return out
}

// membershipTierOptions 等级筛选 / 选择下拉（Selected 同样由服务端算好）。
func membershipTierOptions(c *gin.Context, tiers []*membershipdto.TierResp, selected int64) []gin.H {
	tr := shell.TranslateFor(c)
	out := make([]gin.H, 0, len(tiers)+1)
	out = append(out, gin.H{
		"Value": int64(0), "Label": tr("admin.common.filter.optionAll", "全部"), "Selected": selected == 0,
	})
	for _, tier := range tiers {
		out = append(out, gin.H{"Value": tier.ID, "Label": tier.Name, "Selected": tier.ID == selected})
	}
	return out
}

// entitlementsFromForm 把表单里的两个固定权益字段转成 service 入参。
//
// 「字段不存在 = 这一条权益不设」是有意的：全量保存的语义就是「没列出的会被删掉」，
// 于是「取消免运费」不需要一个单独的删除动作 —— 运营把勾去掉再保存即可。
func entitlementsFromForm(c *gin.Context) []membershipdto.EntitlementReq {
	forms := parseEntitlementsFromForm(c)
	out := make([]membershipdto.EntitlementReq, 0, len(forms))
	for _, form := range forms {
		if !form.Enabled {
			continue
		}
		out = append(out, membershipdto.EntitlementReq{Kind: form.Kind, ValueInt: form.ValueInt})
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
			ID:            tier.ID,
			Name:          tier.Name,
			SortOrder:     tier.SortOrder,
			ThresholdYuan: formatFenToYuan(tier.ThresholdAmount),
			Remark:        tier.Remark,
			IsDefault:     tier.IsDefault,
		}
		for _, ent := range tier.Entitlements {
			switch ent.Kind {
			case membershipenums.KindFreeShipping:
				edit.FreeShipping = ent.ValueInt == 1
			case membershipenums.KindDiscount:
				edit.DiscountPercent = strconv.FormatInt(ent.ValueInt, 10)
			}
		}
		return edit
	}
	return nil
}
