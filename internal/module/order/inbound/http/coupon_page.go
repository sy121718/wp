package orderhttp

// coupon_page.go — 优惠码管理页（列表 / 抽屉表单 / 批量动作）的控制器。
//
// 分工（本页是样板，其余页面照此改造）：
//
//   - **控制器只做三件事**：绑定请求、调 service、渲染响应（整页 / 提示页 / 片段）。
//   - **文案、格式化、显示判断在模板里**：折扣串、门槛、用次、时间窗、状态徽章、
//     按钮该不该出现，全部由模板用 dto 的**原始值**算（money / dateTime / fill / tr 四个全局函数）。
//     Go 侧因此没有 couponRowView / couponDiscountText / couponStatusBadge 这一层 ——
//     它们原先只是「把 dto 改个名 + 预先格式化」，是模板能力不足时的替代品。
//   - **写动作的结论不再走 URL**：成功/失败由 shell.RenderJump 在响应体里渲染提示页；
//     回跳地址由 shell.BackPath 从表单 action 的 query 里按白名单读回来（服务端自己拼，
//     不需要页面预算好再塞进隐藏域）。于是 ?err= / ?ok= / ?done= 那套读侧白名单、
//     couponBackKeys 与十几个回跳函数一起消失。
//
// 三条保持不变的行为契约（改造前后必须一致，逐条都有实测依据）：
//
//  1. **送给 service 的状态筛选是原值**，不是归一化后的值：非法值该被 service 拒绝并在页顶给提示，
//     页面不替它决定「非法 = 不筛」（那会把用户手改的 URL 静默变成「看全部」）。
//  2. **批量启停逐条取回原券再整体回送**：UpdateCoupon 是整体更新，漏送字段会被写成零值
//     （把门槛清零、把时间窗改成不限）—— 那正是「批量停用顺手改坏了券」的成因。
//  3. **批量 id 走 shell.BulkIDs**（去空白 / 去重 / 上限），超限整批拒绝；逐条失败不中断整批，
//     结论按「成功 N / 跳过 M」回带。
//
// 表单的上下文（工程 / 筛选 / 页码）走**表单 action 的 query**，不再用 returnQuery 隐藏域：
// 页面渲染时把 couponListQuery 拼进 action，POST 回来由 shell.BackPath 按白名单读回。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/order/enums"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

// couponPageHandle 优惠码管理页处理器。
type couponPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
}

// NewCouponPageHandle 构造。
func NewCouponPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService) *couponPageHandle {
	return &couponPageHandle{orders: orders, projects: projects}
}

// couponFilter 列表页的筛选状态（GET 参数，全部可选）。
type couponFilter struct {
	// Status 状态展示口径筛选：enabled / disabled / expired / exhausted（原值送给 service）。
	Status string
	// Keyword 关键词（券码 / 名称，服务端决定匹配哪些列）。
	Keyword string
	// CouponID 非 0 时展开这张券的核销记录区（同一个页面靠查询参数切换，不新开路由）。
	CouponID uint64
}

// couponRedemptionPageSize 核销记录区一次列出的条数。
//
// 核销记录区不做独立分页（一张券的核销次数受 MaxUses 约束，展开一屏基本看得完），
// 超过这个数只显示最近一批，并在页面上写明「共 N 条、只显示最近 M 条」。
// 上限同时要 ≤ 200：订单 model 对 limit 的处理是「<=0 或 >200 一律回落到 20」。
const couponRedemptionPageSize = 50

// couponStatusFilterValues 状态筛选下拉的取值白名单（与 CouponListReq.Status 一致）。
//
// 这四个是**展示口径**：服务端把它们翻译成时间与次数条件再下推 —— 过期、未开始、用尽都是
// 时间与次数的函数，coupons 表里并没有这些列。文案不在这里：真源是
// orderenums.CouponStateLabel（口径值 → 词条 key），模板用 key 前缀拼出来取词。
var couponStatusFilterValues = []string{
	orderenums.CouponStateEnabled,
	orderenums.CouponStateDisabled,
	orderenums.CouponStateExpired,
	orderenums.CouponStateExhausted,
}

// couponListQueryKeys 列表页上下文的白名单键。
//
// 一份给「渲染时拼进表单 action」（couponListQuery），一份给「POST 回来时读回上下文」
// （shell.BackPath 的 keys 参数）—— 两处必须是同一份，否则会出现「页面把 couponId 拼进去了、
// 回跳时不认它」这种只在展开态下才暴露的差异。删除动作刻意用不含 couponId 的列表：
// 券没了，回跳时还带着它会让展开区去取一张不存在的券。
var couponListQueryKeys = []string{"project", "status", "keyword", "page", "limit", "couponId"}

// couponDeleteKeys 删除类动作的回跳白名单：**不含 couponId**。
//
// 券没了，回跳时还带着它会让展开区去取一张不存在的券，页面上凭空出现一条「优惠码不存在」
// —— 那是操作成功的下一秒，用户会以为删除失败了。
var couponDeleteKeys = []string{"project", "status", "keyword", "page", "limit"}

// CouponsPage 优惠码管理页（GET /admin/coupons）。
func (h *couponPageHandle) CouponsPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
	// 页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留 —— 运营看得出「是这一页没读出来」。
	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	// 装载失败时不再去读列表 / 展开区：工程上下文都没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查券，查出来的是哪个工程的券都说不清。
	selected := ""
	if loadErr == nil {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	page, limit := orderListWindow(c)
	filter := couponFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		CouponID: orderQueryID(c.Query("couponId")),
	}
	// 状态筛选的**生效值**：白名单内归一成规范写法（service 也按小写匹配），白名单外原样留着
	// 并把 valid 置 false —— 控件回显、空态分支与「清掉筛选」的判据都取自它。
	filterStatusValue, filterStatusValid := couponFilterStatusValue(filter.Status)

	var coupons []*orderdto.CouponResp
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListCoupons(ctx, &orderdto.CouponListReq{
			ProjectID: selected,
			Keyword:   filter.Keyword,
			Status:    filter.Status,
			Offset:    (page - 1) * limit,
			Limit:     limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, lerr))
		} else {
			coupons, total = list.List, list.Total
		}
	}

	// 展开区（核销记录）：只读，不含编辑表单 —— 编辑进右侧抽屉，按需 GET 片段。
	var expanded *orderdto.CouponResp
	var redemptions []*orderdto.CouponRedemptionResp
	var redemptionTotal int64
	if selected != "" && filter.CouponID > 0 {
		cp, gerr := h.orders.GetCoupon(ctx, filter.CouponID)
		switch {
		case gerr != nil:
			loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, gerr))
		case cp == nil || cp.ProjectID != selected:
			// couponId 与 project 是两个独立参数，切了工程之后 URL 上可能还留着一张属于别的
			// 工程的券。既不展开它，也不静默忽略 —— 静默忽略会让「点了修改没反应」变成一个
			// 查不出来的现象。
			loadErrText = firstNonEmpty(loadErrText, couponFacing(c, couponForeignLabel))
		default:
			expanded = cp
			rl, rerr := h.orders.ListCouponRedemptions(ctx, &orderdto.CouponRedemptionListReq{
				ProjectID: selected,
				CouponID:  filter.CouponID,
				Offset:    0,
				Limit:     couponRedemptionPageSize,
			})
			if rerr != nil {
				loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, rerr))
			} else {
				redemptions, redemptionTotal = rl.List, rl.Total
			}
		}
	}

	data := shell.Prepare(c, gin.H{
		// 标题是 i18n key：shell.Prepare 会按当前语言取词（模板不再看到裸 key）。
		"title": "admin.coupons.heading",
		"menu":  "coupons",
		// 取数结果：dto 原样交给模板，字段名就是模板要用的名字。
		"Projects":        projects,
		"SelectedProject": selected,
		"Coupons":         coupons,
		"Total":           total,
		"Expanded":        expanded,
		"Redemptions":     redemptions,
		"RedemptionTotal": redemptionTotal,
		"RedemptionLimit": couponRedemptionPageSize,
		// 筛选：白名单值给模板画下拉，原值给回显（白名单外的值要原样显示，不能悄悄变成「全部」）。
		"StatusValues":      couponStatusFilterValues,
		"FilterStatus":      filterStatusValue,
		"FilterStatusValid": filterStatusValid,
		"FilterKeyword":     filter.Keyword,
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有优惠码」分开。
		"LoadErr": loadErrText,
		// 列表上下文（拼进表单 action 的 query）与新建表单的回填槽（首屏为空）。
		"ListQuery": couponListQuery(selected, filter, page, limit),
		"Echo":      url.Values{},
		"SubmitErr": "",
		"Page":      page,
		"Limit":     limit,
	})
	base := shell.WithParams("/admin/coupons", map[string]string{
		"project": selected, "status": filter.Status, "keyword": filter.Keyword,
	})
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/order/coupons.html", data)
}

// CouponEditForm 按需返回一张券的编辑抽屉内容（GET /admin/coupons/edit-form）。
//
// 路由复用 POST 更新权限；参数与券归属在响应片段之前校验。
// 首屏回填用 **dto 的值**（Echo 为空），模板的 formValue(echo, name, dtoValue) 会自动选源。
func (h *couponPageHandle) CouponEditForm(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	rawID := c.Query("id")
	id := orderQueryID(rawID)
	projectID := strings.TrimSpace(c.Query("project"))
	if id == 0 || strconv.FormatUint(id, 10) != rawID || projectID == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	ctx := c.Request.Context()
	exists, err := h.projects.Exists(ctx, projectID)
	if err != nil {
		shell.PageError(c, "coupon-edit-form", err)
		return
	}
	if !exists {
		c.Status(http.StatusNotFound)
		return
	}
	cp, err := h.orders.GetCoupon(ctx, id)
	if err != nil {
		if err.Error() == orderenums.ErrCouponNotFound {
			c.Status(http.StatusNotFound)
			return
		}
		shell.PageError(c, "coupon-edit-form", err)
		return
	}
	if cp == nil || cp.ID != id || cp.ProjectID != projectID {
		c.Status(http.StatusNotFound)
		return
	}

	c.HTML(http.StatusOK, "admin/order/coupon_edit_form.html", shell.Prepare(c, gin.H{
		"Coupon":    cp,
		"Echo":      url.Values{},
		"SubmitErr": "",
		"ListQuery": couponListQuery(projectID, couponFilter{Status: c.Query("status"), Keyword: c.Query("keyword")}, 0, 0),
	}))
}

// CouponCreate 新建优惠码（POST /admin/coupons/create）。
//
// 失败分档：htmx 提交 → 200 + 表单片段自身（错误槽 + 回填，抽屉里原地留住已填内容）；
// 其余 → 提示页。成功一律走提示页（htmx 档 RenderJump 内部换成 HX-Redirect）。
func (h *couponPageHandle) CouponCreate(c *gin.Context) {
	req := couponSaveReqFromForm(c)
	if req.ProjectID == "" {
		h.couponCreateFail(c, orderenums.ErrProjectRequired)
		return
	}
	if _, err := h.orders.CreateCoupon(c.Request.Context(), req); err != nil {
		h.couponCreateFail(c, couponFacingError(c, err))
		return
	}
	// 不回跳展开新券：新建表单里没有 couponId，运营接下来多半是接着建下一张。
	h.couponDone(c, orderenums.MsgCouponCreated)
}

// CouponUpdate 修改 / 停用 / 启用（POST /admin/coupons/update）。
//
// 停用与启用复用本方法（表单里带 status）：它们与「改门槛」「改时间窗」是同一份
// 「券的整体配置」，单独开一个状态接口只会多出一条「改状态时忘了回送门槛」的路径 ——
// UpdateCoupon 是整体更新，漏送的字段会被写成零值。
func (h *couponPageHandle) CouponUpdate(c *gin.Context) {
	req := couponSaveReqFromForm(c)
	if req.ID == 0 {
		h.couponEditFail(c, couponFacing(c, couponIDInvalidLabel))
		return
	}
	if req.ProjectID == "" {
		h.couponEditFail(c, orderenums.ErrProjectRequired)
		return
	}
	if _, err := h.orders.UpdateCoupon(c.Request.Context(), req); err != nil {
		h.couponEditFail(c, couponFacingError(c, err))
		return
	}
	h.couponDone(c, orderenums.MsgCouponUpdated)
}

// CouponDelete 删除优惠码（POST /admin/coupons/delete）。
//
// 有核销记录的券会被服务端拒绝（「该优惠码已有核销记录，不能删除」）：删了记录就指向一张
// 查不到的券，对账时分不清是数据坏了还是券被删了。页面照原样回显服务端给的原因。
//
// 回跳不认 couponId（见 couponDeleteKeys）：券没了，展开区再去取它就是一条假错误。
func (h *couponPageHandle) CouponDelete(c *gin.Context) {
	id := orderQueryID(c.PostForm("id"))
	if id == 0 {
		h.couponFail(c, couponFacing(c, couponIDInvalidLabel), couponDeleteKeys...)
		return
	}
	if err := h.orders.DeleteCoupon(c.Request.Context(), id); err != nil {
		h.couponFail(c, couponFacingError(c, err), couponDeleteKeys...)
		return
	}
	h.couponDone(c, orderenums.MsgCouponDeleted, couponDeleteKeys...)
}

// CouponBulkDelete 批量删除（POST /admin/coupons/bulk-delete）。
//
// 逐条走同一条单条删除路径：有核销记录的券由服务端拒绝，只跳过它并计入跳过数，其余照常删除。
func (h *couponPageHandle) CouponBulkDelete(c *gin.Context) {
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.couponFail(c, shell.BulkIDsFacingText(c, berr), couponDeleteKeys...)
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.orders.DeleteCoupon(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	h.couponDone(c, bulkSummary(c, orderBulkVerbDeleted, orderBulkNounCoupon, deleted, skipped), couponDeleteKeys...)
}

// CouponBulkToggle 批量停用 / 启用（POST /admin/coupons/bulk-toggle，表单带目标状态 status）。
func (h *couponPageHandle) CouponBulkToggle(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	target, ok := couponToggleTarget(c.PostForm("status"))
	if !ok {
		h.couponFail(c, orderBulkTextOf(c, couponBulkTargetInvalidText))
		return
	}
	verb := orderBulkVerbDisabled
	if target == 1 {
		verb = orderBulkVerbEnabled
	}
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.couponFail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	changed, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		cp, gerr := h.orders.GetCoupon(c.Request.Context(), id)
		if gerr != nil || cp == nil {
			skipped++
			continue
		}
		// 表单里的 couponId 与 projectId 是两个独立字段，切了工程之后可能还留着别的工程的券：
		// 越界的那张不处理，而不是替用户跨工程改一张他看不见的券。
		if projectID != "" && cp.ProjectID != projectID {
			skipped++
			continue
		}
		_, uerr := h.orders.UpdateCoupon(c.Request.Context(), &orderdto.CouponSaveReq{
			ID:            cp.ID,
			ProjectID:     cp.ProjectID,
			Code:          cp.Code,
			Name:          cp.Name,
			DiscountType:  cp.DiscountType,
			DiscountValue: cp.DiscountValue,
			MinSubtotal:   cp.MinSubtotal,
			MaxUses:       cp.MaxUses,
			PerUserLimit:  cp.PerUserLimit,
			// 时间窗原样回送：格式化与模板侧的 dateTimeLocal 共用同一份实现
			// （两处各写一份，漂移的表现是「批量操作把时间窗改成了别的时刻」）。
			StartsAt: templates.DateTimeLocal(cp.StartsAt),
			EndsAt:   templates.DateTimeLocal(cp.EndsAt),
			Status:   target,
			Remark:   cp.Remark,
			// 操作人由会话覆盖写入，绝不受表单影响。
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if uerr != nil {
			skipped++
			continue
		}
		changed++
	}
	h.couponDone(c, bulkSummary(c, verb, orderBulkNounCoupon, changed, skipped))
}

// —— 写动作的三个出口 ——

// couponDone 写成功：提示页（htmx 档由 RenderJump 换成 HX-Redirect）。
func (h *couponPageHandle) couponDone(c *gin.Context, msg string, keys ...string) {
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      msg,
		Back:     h.back(c, keys...),
		BackText: couponBackText(c),
		Seconds:  1,
	})
}

// couponFail 无表单的写失败（删除 / 批量）：提示页，不自动跳转（用户要看清原因）。
func (h *couponPageHandle) couponFail(c *gin.Context, msg string, keys ...string) {
	shell.RenderJump(c, shell.Jump{
		OK:       false,
		Msg:      msg,
		Back:     h.back(c, keys...),
		BackText: couponBackText(c),
	})
}

// couponCreateFail 新建失败：htmx 档就地重渲表单片段（回填 + 错误槽），其余走提示页。
func (h *couponPageHandle) couponCreateFail(c *gin.Context, msg string) {
	if !shell.IsHXRequest(c) {
		h.couponFail(c, msg)
		return
	}
	c.HTML(http.StatusOK, "admin/order/coupon_create_form.html", shell.Prepare(c, gin.H{
		// 原始提交值（含空值）：回填要的是「用户刚打的字」，不是「库里的旧值」。
		"Echo":            couponEcho(c),
		"SubmitErr":       msg,
		"SelectedProject": strings.TrimSpace(c.PostForm("projectId")),
		"ListQuery":       couponQueryFromRequest(c, couponListQueryKeys...),
	}))
}

// couponEditFail 修改失败：htmx 档就地重渲编辑片段（回填 + 错误槽），其余走提示页。
func (h *couponPageHandle) couponEditFail(c *gin.Context, msg string) {
	if !shell.IsHXRequest(c) {
		h.couponFail(c, msg)
		return
	}
	// 片段要显示券码（只读）与工程，所以要按 id 把券取回来；取不到就退回提示页。
	id := orderQueryID(c.PostForm("id"))
	cp, err := h.orders.GetCoupon(c.Request.Context(), id)
	if err != nil || cp == nil {
		h.couponFail(c, msg)
		return
	}
	c.HTML(http.StatusOK, "admin/order/coupon_edit_form.html", shell.Prepare(c, gin.H{
		"Coupon":    cp,
		"Echo":      couponEcho(c),
		"SubmitErr": msg,
		"ListQuery": couponQueryFromRequest(c, couponListQueryKeys...),
	}))
}

// —— 绑定与请求工具 ——

// couponBackText 提示页那个链接的文字。
//
// 复用页面标题词条（「优惠码管理」）而不是新造一条 `*.action.back`：新增词条要走 seed 迁移，
// 而这一句的语义就是「去这一页」。要改成「返回优惠码列表」时，加词条 + 迁移即可。
func couponBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.coupons.heading", "优惠码管理")
}

// back 读回跳上下文（表单 action 的 query）并拼出站内地址。
func (h *couponPageHandle) back(c *gin.Context, keys ...string) string {
	return shell.BackPath(c, "/admin/coupons", keys...)
}

// couponQueryFromRequest 本次请求 query 里的列表上下文（表单 action 上带回来的那一段）。
//
// 与 couponListQuery 的分工：那个是**渲染时**从 controller 已知的值拼（列表页首屏），
// 这个是**写失败重渲片段时**从请求里读回来（POST 的 action query 就带着上下文）。
// 两者产出的形状必须一致，否则「首屏表单 action 带 A、失败重渲后带 B」。
func couponQueryFromRequest(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	out := url.Values{}
	for _, k := range keys {
		if v := strings.TrimSpace(in.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

// couponListQuery 列表上下文 → 查询串（拼进表单 action / 抽屉 URL；空值不拼）。
func couponListQuery(projectID string, filter couponFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("status", filter.Status)
	set("keyword", filter.Keyword)
	if filter.CouponID > 0 {
		set("couponId", strconv.FormatUint(filter.CouponID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

// couponEcho 本次提交的表单快照（失败片段回填用）。
//
// 走标准库的解析入口（gin 的 PostForm 也走这里），urlencoded 与 multipart 都会填进 PostForm；
// 解析错误一律忽略 —— 解析失败就当「没提交」，回填退化成空表单，提交是否合法由业务校验回答。
func couponEcho(c *gin.Context) url.Values {
	if c == nil || c.Request == nil {
		return url.Values{}
	}
	_ = c.Request.ParseMultipartForm(couponEchoMemory)
	if c.Request.PostForm == nil {
		return url.Values{}
	}
	return c.Request.PostForm
}

// couponEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const couponEchoMemory = 32 << 20

// couponFilterStatusValue 状态筛选的**生效值**：把 URL 上的 status 归一到白名单的写法。
//
// 归一化不是洁癖，是两处实测行为逼出来的：
//
//   - service 的匹配是 strings.ToLower + TrimSpace（见 ListCoupons），所以 ?status=ENABLED
//     真的按「生效中」筛过。若把 URL 原值直接交给模板，下拉里的 `value == filterStatus` 比较
//     失败 → 控件回显「（状态：全部）」而列表已经是筛过的结果：控件与生效筛选自相矛盾。
//   - 不在白名单里的值不能悄悄换成空串：service 会以「参数不合法」拒绝这次筛选（页顶有提示），
//     URL 上的值仍是用户当前的上下文。返回 ok=false 让模板把它**原样回显**出来。
func couponFilterStatusValue(raw string) (value string, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	for _, v := range couponStatusFilterValues {
		if v == s {
			return v, true
		}
	}
	return strings.TrimSpace(raw), false
}

// couponToggleTarget 批量启停的目标状态：只认 1（启用）/ 0（停用），其余一律不合法。
//
// 不把非法值归一成 0：归一等于「填错就悄悄把券全停了」，而运营看到的是「批量启用成功」。
func couponToggleTarget(raw string) (int, bool) {
	switch strings.TrimSpace(raw) {
	case "1":
		return 1, true
	case "0":
		return 0, true
	}
	return 0, false
}

// couponSaveReqFromForm 从表单读取优惠码保存请求。
//
// 字段名必须与 CouponSaveReq 的 form tag 逐字一致：写错不会报错，只会绑定到零值
// （表现是「时间窗填了却被当成不限」「门槛被清零」这类静默的错误数据）。
func couponSaveReqFromForm(c *gin.Context) *orderdto.CouponSaveReq {
	return &orderdto.CouponSaveReq{
		ID:            orderQueryID(c.PostForm("id")),
		ProjectID:     strings.TrimSpace(c.PostForm("projectId")),
		Code:          strings.TrimSpace(c.PostForm("code")),
		Name:          strings.TrimSpace(c.PostForm("name")),
		DiscountType:  strings.TrimSpace(c.PostForm("discountType")),
		DiscountValue: couponFormInt64(c, "discountValue"),
		MinSubtotal:   couponFormInt64(c, "minSubtotal"),
		MaxUses:       couponFormInt(c, "maxUses"),
		PerUserLimit:  couponFormInt(c, "perUserLimit"),
		StartsAt:      couponFormTimeValue(c.PostForm("startsAt")),
		EndsAt:        couponFormTimeValue(c.PostForm("endsAt")),
		Status:        couponFormInt(c, "status"),
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}
}

// couponFormTimeValue 归一化时间窗表单值，把控件提交的写法换成服务端认识的写法。
//
// `<input type="datetime-local">` 提交的是它自己的 value 形态 ——「2026-01-01T09:00」（带 T），
// 而优惠码服务端的解析布局只有「2006-01-02」/「2006-01-02 15:04(:05)」，带 T 的写法会被判成
// 「生效时间不合法」。不在这儿转换，就是**表单自己生成的格式自己都不收**。
//
// 做法是「解析成功才重新格式化」而不是字符串替换 —— 位置判断会把畸形串也当成可转；
// 解析失败则原样透传（空串 = 不限、纯日期与空格写法都是既有客户端的合法输入）。
func couponFormTimeValue(raw string) string {
	s := strings.TrimSpace(raw)
	// 带秒的输入保留秒（服务端同样接受「2006-01-02 15:04:05」）——
	// 统一截断到分等于静默改掉用户填的时刻。
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return s
}

// —— 文案与错误出口（优惠码模块的白名单）——

// 券页可原样展示的**管理侧**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），后台管理侧
// 独有的文案不在其中：券码重了、折扣类型 / 折扣值不合法、时间窗不合法、有核销记录不能删，
// 以及三条成功提示 —— 不补这一层，运营点下去只会看到「系统内部错误，请稍后重试」，
// 而真正的原因（「这个优惠码已经存在」）就丢了。
var couponFacingExtras = []string{
	orderenums.MsgCouponCreated, orderenums.MsgCouponUpdated, orderenums.MsgCouponDeleted,
	orderenums.ErrCouponCodeTaken, orderenums.ErrCouponTypeInvalid, orderenums.ErrCouponValueInvalid,
	orderenums.ErrCouponWindowInvalid, orderenums.ErrCouponInUse,
	couponIDInvalidLabel.fallback, couponForeignLabel.fallback,
}

// 券页自造的两条文案（key + 中文兜底）。
var (
	// couponIDInvalidLabel 表单里的券 id 不合法。
	couponIDInvalidLabel = orderLabel{"admin.coupons.form.invalid_id", "优惠码编号不合法，请回到列表页重新操作。"}
	// couponForeignLabel couponId 指向的是别的工程的券。
	couponForeignLabel = orderLabel{"admin.coupons.form.foreign_project", "这张优惠码不属于当前选中的站点工程，请切回它所属的工程再操作。"}
)

// couponBulkTargetInvalidText 批量启停的目标状态不合法时的回执。
var couponBulkTargetInvalidText = orderBulkText{orderenums.BulkCouponTargetInvalid, "目标状态不合法，本次没有处理任何优惠码。"}

// couponFacing 取词（key + 中文兜底 → 当前语言的成品文案）。
func couponFacing(c *gin.Context, l orderLabel) string {
	return orderLabelOf(shell.TranslateFor(c), l)
}

// couponFacingError 把 service 的错误收敛成可展示文案：命中白名单取词展示，
// 未命中记结构化日志并给归口文案（原文只进日志）。
func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := couponFacingText(err.Error()); msg != "" {
		return shell.TranslateFor(c)(msg, msg)
	}
	logger.Scene("order-coupon").With("path", c.Request.URL.Path).Error(err, "优惠码写操作失败")
	return shell.PageInternalText(c)
}

// couponFacingText 白名单判定：命中返回原文，未命中返回空串。
//
// 判定只有这一份（API 出口要 key、页面出口要成品文案，但「哪些能透出」是同一件事）。
func couponFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range couponFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// couponFormInt 读取整数表单字段；缺失 / 非法一律当 0（由服务端业务规则决定 0 是否可接受）。
//
// 负数**不在这里吞掉**：填了 -5 就应该让服务端报「优惠值不合法」，
// 静默改成 0 会让运营以为保存成功了，而券的配置并不是他填的那个。
func couponFormInt(c *gin.Context, key string) int {
	v, err := strconv.Atoi(strings.TrimSpace(c.PostForm(key)))
	if err != nil {
		return 0
	}
	return v
}

// couponFormInt64 读取 int64 表单字段（金额一律整数分）。
func couponFormInt64(c *gin.Context, key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(c.PostForm(key)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
