package orderhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/web/shell"
)

// coupon_page_handle.go — 后台优惠码管理页（BIZ-1 销售侧）。

//

// 优惠码模块的后台 API 已就绪（7 个接口挂在 /api/order/coupon/* 的三层链上：

// Session + CSRF + Casbin），但后台没有管理界面 —— 运营看不到也建不了券。

// 本文件补齐这个入口：工程切换 + 组合筛选 + 列表 + 新建 + 行内修改

// （同一页面靠 couponId 查询参数展开，不新开页面路由）+ 停用 / 启用 + 删除 + 核销记录。

// **无 JS 也能用**：所有链接都是普通 GET，所有写操作都是原生表单 POST + csrf_token 隐藏域。

//

// 四条与优惠码模块的约定：

//

//  1. 跨模块只依赖 ordercontract.OrderService 与不可变 orderdto，

//     **不 import 订单模块的 model / service**（CouponService 已嵌在 OrderService 里）。

//

//  2. **没有、也不应该有「核销」按钮**：核销发生在建单事务内部

//     （扣次数、插核销明细与写订单同生共死），单独暴露一个「先核销、后建单」的入口

//     一定会被用出「券没了但没下单」这种状态。本页只展示核销**结果**（按 couponId 列记录）。

//

//  3. 金额与文案的换算**只在服务端发生一次**：DiscountLabel / MinSubtotalLabel / StatusLabel

//     都是 CouponResp 里算好的展示字段，handler 与模板都不做任何金额换算 ——

//     两处换算迟早会分叉，而券的力度分叉出来就是客诉。

//     「已用 / 上限」「每人限次」「时间窗」这类分支也在这里拼成文本，模板只负责摆放。

//

//  4. 操作人（OperatorID / OperatorName）由 handler 从会话覆盖写入，

//     表单里不存在这两个字段：能被客户端伪造的操作人，等于审计上没有操作人。

const (
	// couponPageTitle 页面标题（订单模块 enums 里没有这个标题键，直接走 shell.Prepare 的 fallback 链路）。
	couponPageTitle = "优惠码管理"
	// couponRedemptionPageSize 核销记录区一次列出的条数。
	//
	// 核销记录区不做独立分页（一张券的核销次数受 MaxUses 约束，展开一屏基本看得完），
	// 超过这个数只显示最近一批，并在页面上写明「共 N 条、只显示最近 M 条」。
	// 上限同时要 ≤ 200：订单 model 对 limit 的处理是「<=0 或 >200 一律回落到 20」。
	couponRedemptionPageSize = 50
	// couponUnlimitedLabel 「不限」口径的展示文案（次数上限 / 每人限次 / 时间窗共用）。
	couponUnlimitedLabel = "不限"
	// couponIDInvalidText 表单里的券 id 不合法（本页自造文案，已进本页白名单）。
	couponIDInvalidText = "优惠码编号不合法，请回到列表页重新操作。"
	// couponForeignText couponId 指向的是别的工程的券（本页自造文案，已进本页白名单）。
	couponForeignText = "这张优惠码不属于当前选中的站点工程，请切回它所属的工程再操作。"
)

// couponStatusFilters 状态筛选下拉的取值白名单（与 CouponListReq.Status 一致）。
//
// 这四个是**展示口径**：服务端把它们翻译成时间与次数条件再下推 ——
// 过期、未开始、用尽都是时间的函数，coupons 表里并没有这些列。
var couponStatusFilters = []struct {
	Value string
	Label string
}{
	{"enabled", "生效中"},
	{"disabled", "已停用"},
	{"expired", "已过期"},
	{"exhausted", "已用完"},
}

// couponStatusBadges 状态**文案** → 徽章样式。
//
// 键是服务端算好的 StatusLabel 而不是状态列的取值（1/0）：一张启用的券是「生效中」
// 还是「已过期」，取决于当前时间与已用次数，页面自己再算一遍迟早会和服务端分叉。
var couponStatusBadges = map[string]string{
	"生效中": "badge-success",
	"未开始": "badge-warning",
	"已用完": "badge-mute",
	"已过期": "badge-mute",
	"已停用": "badge-danger",
}

// couponTypeOptions 折扣类型下拉（与 CouponSaveReq.DiscountType 的白名单一致）。
var couponTypeOptions = []struct {
	Value string
	Label string
}{
	{"percent", "按比例（折扣力度 1..100）"},
	{"fixed", "固定金额（单位：分）"},
}

// couponEnableOptions 启停状态下拉（表单字段 Status：1 启用 / 0 停用）。
var couponEnableOptions = []struct {
	Value string
	Label string
}{
	{"1", "启用"},
	{"0", "停用"},
}

// couponFacingExtras 本页可原样展示的**管理侧**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），
// 后台管理侧独有的文案不在其中：券码重了、折扣类型 / 折扣值不合法、时间窗不合法、
// 有核销记录不能删，以及三条成功提示 —— 不补这一层，运营点下去只会看到
// 「系统内部错误，请稍后重试」，而真正的原因（「这个优惠码已经存在」）就丢了。
//
// 后两条是本页自造的文案：?err= 是用户可编辑的查询参数，回显时同样要过白名单，
// 自造文案不登记在这里就等着被自己吞掉。
var couponFacingExtras = []string{
	orderenums.MsgCouponCreated, orderenums.MsgCouponUpdated, orderenums.MsgCouponDeleted,
	orderenums.ErrCouponCodeTaken, orderenums.ErrCouponTypeInvalid, orderenums.ErrCouponValueInvalid,
	orderenums.ErrCouponWindowInvalid, orderenums.ErrCouponInUse,
	couponIDInvalidText, couponForeignText,
}

// couponPageHandle 优惠码管理页处理器。
type couponPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
}

// NewCouponPageHandle 构造。
func NewCouponPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService) *couponPageHandle {
	return &couponPageHandle{orders: orders, projects: projects}
}

// couponFilter 页面筛选条件（GET 参数，全部可选）。
type couponFilter struct {
	// Status 状态展示口径筛选（空 = 全部）：enabled / disabled / expired / exhausted。
	Status string
	// Keyword 关键词（券码 / 名称，服务端决定匹配哪些列）。
	Keyword string
	// CouponID 非 0 时在列表下方渲染编辑区与核销记录区
	//（同一个页面靠查询参数切换，不新开路由）。
	CouponID uint64
}

// CouponsPage 优惠码管理页（GET /admin/coupons）。
func (h *couponPageHandle) CouponsPage(c *gin.Context) {
	ctx := c.Request.Context()

	// 回显文案：?err= / ?ok= 都过白名单，查不到的一律收口
	//（查询参数是用户可编辑的，不能拿它当「业务提示」直接显示）。
	// 取词出口是 couponPageFacingText：白名单里存的是 item_key，页面模板直接渲染
	// {{.Err}} / {{.Ok}}，不取词的话提示条上就是那串裸 key。
	// 先于装载计算：装载失败要**压过**它（见下）。
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), couponPageFacingText(c))
	pageOk := shell.FacingQueryText(c.Query("ok"), "", couponPageFacingText(c))

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据见 order_page_handle.go 的 OrdersPage）：
	// 空列表 + 归口提示 + HTTP 200，页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
	// 装载失败**压过 ?err=**：它是这次请求真实发生的事。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = orderFacingError(c, loadErr)
	}

	// 装载失败时不再去读列表 / 展开区：工程上下文都没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查券，查出来的是哪个工程的券都说不清。
	selected := ""
	if !loadFailed {
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

	rows := []gin.H{}
	redemptions := []gin.H{}
	detail := gin.H{}
	var total, redemptionTotal int64

	if selected != "" {
		list, lerr := h.orders.ListCoupons(ctx, &orderdto.CouponListReq{
			ProjectID: selected,
			Keyword:   filter.Keyword,
			Status:    filter.Status,
			Offset:    (page - 1) * limit,
			Limit:     limit,
		})
		if lerr != nil {
			pageErr = firstNonEmpty(pageErr, couponFacingError(c, lerr))
		} else {
			total = list.Total
			for _, cp := range list.List {
				rows = append(rows, couponRowView(cp, filter, selected, page, limit))
			}
		}

		if filter.CouponID > 0 {
			cp, gerr := h.orders.GetCoupon(ctx, filter.CouponID)
			switch {
			case gerr != nil:
				pageErr = firstNonEmpty(pageErr, couponFacingError(c, gerr))
			case cp == nil || cp.ProjectID != selected:
				// couponId 与 project 是两个独立参数，切了工程之后 URL 上可能还留着一张
				// 属于别的工程的券。既不展开它，也不静默忽略 —— 静默忽略会让
				// 「点了修改没反应」变成一个查不出来的现象。
				pageErr = firstNonEmpty(pageErr, couponForeignText)
			default:
				detail = couponEditView(cp, filter, selected, page, limit)
				rl, rerr := h.orders.ListCouponRedemptions(ctx, &orderdto.CouponRedemptionListReq{
					ProjectID: selected,
					CouponID:  filter.CouponID,
					Offset:    0,
					Limit:     couponRedemptionPageSize,
				})
				if rerr != nil {
					pageErr = firstNonEmpty(pageErr, couponFacingError(c, rerr))
				} else {
					redemptionTotal = rl.Total
					for _, rd := range rl.List {
						redemptions = append(redemptions, couponRedemptionRow(rd))
					}
				}
			}
		}
	}

	// 「收起展开区」链接：丢掉 couponId，其余筛选与窗口照旧。
	collapse := couponFilterValues(selected, filter)
	collapse["page"] = strconv.Itoa(page)
	collapse["limit"] = strconv.Itoa(limit)

	data := shell.Prepare(c, gin.H{
		"title":           couponPageTitle,
		"menu":            "coupons",
		"Projects":        projects,
		"SelectedProject": selected,
		"FilterOptions":   couponStatusFilters,
		"TypeOptions":     couponTypeOptions,
		"StatusOptions":   couponEnableOptions,
		"FilterStatus":    filter.Status,
		"FilterKeyword":   filter.Keyword,
		"Rows":            rows,
		"Total":           total,
		"Detail":          detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这两个键决定渲不渲染展开区。
		"HasDetail": len(detail) > 0,
		// 同上：装载失败时空态必须与「这个工程还没有优惠码」区分开，判据由 handler 算好。
		"LoadFailed":        loadFailed,
		"DetailCollapseURL": shell.FilterBaseURL("/admin/coupons", collapse),
		// 新建完成后回到**同一个工程**（不带 couponId：还没有展开任何券）。
		"CreateBack":      couponBackQuery(selected, filter, page, limit, 0),
		"Redemptions":     redemptions,
		"RedemptionTotal": redemptionTotal,
		"RedemptionLimit": couponRedemptionPageSize,
		"Page":            page,
		"Limit":           limit,
		"Err":             pageErr,
		"Ok":              pageOk,
		// 批量动作的结论：数量是动态的，过不了 ?ok= / ?err= 的文案白名单，单独走 ?done=。
		"Done": orderPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/coupons", couponFilterValues(selected, filter))
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/order/coupons.html", data)
}

// CouponCreate 新建优惠码（POST /admin/coupons/create）。
func (h *couponPageHandle) CouponCreate(c *gin.Context) {
	req := couponSaveReqFromForm(c)
	if req.ProjectID == "" {
		couponRedirect(c, "", orderenums.ErrProjectRequired)
		return
	}
	if _, err := h.orders.CreateCoupon(c.Request.Context(), req); err != nil {
		couponRedirect(c, "", couponFacingError(c, err))
		return
	}
	// 不回跳展开新券：新建表单里没有 couponId，运营接下来多半是接着建下一张。
	couponRedirect(c, orderenums.MsgCouponCreated, "")
}

// CouponUpdate 修改 / 停用 / 启用（POST /admin/coupons/update）。
//
// 停用与启用复用本方法（表单里带 status）：它们与「改门槛」「改时间窗」是同一份
// 「券的整体配置」，单独开一个状态接口只会多出一条「改状态时忘了回送门槛」的路径 ——
// UpdateCoupon 是整体更新，漏送的字段会被写成零值。
func (h *couponPageHandle) CouponUpdate(c *gin.Context) {
	req := couponSaveReqFromForm(c)
	if req.ID == 0 {
		couponRedirect(c, "", couponIDInvalidText)
		return
	}
	if req.ProjectID == "" {
		couponRedirect(c, "", orderenums.ErrProjectRequired)
		return
	}
	if _, err := h.orders.UpdateCoupon(c.Request.Context(), req); err != nil {
		couponRedirect(c, "", couponFacingError(c, err))
		return
	}
	couponRedirect(c, orderenums.MsgCouponUpdated, "")
}

// CouponDelete 删除优惠码（POST /admin/coupons/delete）。
//
// 有核销记录的券会被服务端拒绝（「该优惠码已有核销记录，不能删除」）：
// 删了记录就指向一张查不到的券，对账时分不清是数据坏了还是券被删了。
// 页面照原样回显服务端给的中文原因，不自己另编一套。
func (h *couponPageHandle) CouponDelete(c *gin.Context) {
	id := orderQueryID(c.PostForm("id"))
	if id == 0 {
		couponRedirect(c, "", couponIDInvalidText)
		return
	}
	if err := h.orders.DeleteCoupon(c.Request.Context(), id); err != nil {
		couponRedirect(c, "", couponFacingError(c, err))
		return
	}
	// 券没了，回跳时丢掉 couponId：否则展开区会去取一张已经不存在的券并报「优惠码不存在」。
	couponRedirectSkip(c, orderenums.MsgCouponDeleted, "", "couponId")
}

// CouponBulkDelete 批量删除优惠码（POST /admin/coupons/bulk-delete）。
//
// 逐条走同一条单条删除路径：有核销记录的券由服务端拒绝（删了记录就指向一张查不到的券，
// 对账时分不清是数据坏了还是券被删了），只跳过它并计入跳过数，其余照常删除。
func (h *couponPageHandle) CouponBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		couponRedirect(c, "", orderBulkIDsText(c, berr))
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
	// 券没了，回跳时丢掉 couponId：否则展开区会去取一张已经不存在的券并报「优惠码不存在」。
	couponBulkRedirectSkip(c, bulkSummary(c, orderBulkVerbDeleted, orderBulkNounCoupon, deleted, skipped), "couponId")
}

// CouponBulkToggle 批量停用 / 启用（POST /admin/coupons/bulk-toggle，表单带目标状态 status）。
//
// 与单条启停走同一个 UpdateCoupon —— 它是**整体更新**，所以这里逐条先取当前券、
// 把全部可改字段原样回送、只改 status：漏送字段会被静默写成零值（把门槛清零、
// 把时间窗改成不限），而那正是「批量停用顺手改坏了券」的成因。
func (h *couponPageHandle) CouponBulkToggle(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	target, ok := couponToggleTarget(c.PostForm("status"))
	if !ok {
		couponBulkRedirect(c, orderBulkTextOf(c, couponBulkTargetInvalidText))
		return
	}
	// 动词也是词条（order.bulk.verb.disabled / enabled）：语序不同，不能只翻模板。
	verb := orderBulkVerbDisabled
	if target == 1 {
		verb = orderBulkVerbEnabled
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		couponRedirect(c, "", orderBulkIDsText(c, berr))
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
			StartsAt:      couponFormTime(cp.StartsAt.TimePtr()),
			EndsAt:        couponFormTime(cp.EndsAt.TimePtr()),
			Status:        target,
			Remark:        cp.Remark,
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
	couponBulkRedirect(c, bulkSummary(c, verb, orderBulkNounCoupon, changed, skipped))
}

// couponBulkTargetInvalidText 批量启停的目标状态不合法时的回执（走 ?done=，因此与批量结论一起登记）。
var couponBulkTargetInvalidText = orderBulkText{orderenums.BulkCouponTargetInvalid, "目标状态不合法，本次没有处理任何优惠码。"}

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

// couponBulkRedirect 批量动作回列表页：结论走 ?done=。
func couponBulkRedirect(c *gin.Context, doneText string) {
	couponBulkRedirectSkip(c, doneText, "")
}

// couponBulkRedirectSkip 同上，但先丢掉一个回跳参数（批量删除后必须丢掉 couponId）。
func couponBulkRedirectSkip(c *gin.Context, doneText, skipKey string) {
	q := url.Values{}
	if parsed, err := url.ParseQuery(strings.TrimSpace(c.PostForm("returnQuery"))); err == nil {
		// 只透传白名单键：returnQuery 同样来自客户端，不能让它往回跳 URL 里塞任意参数。
		for key, vals := range parsed {
			if _, ok := couponBackKeys[key]; ok && len(vals) > 0 {
				q.Set(key, vals[0])
			}
		}
	}
	q.Del("ok")
	q.Del("err")
	if skipKey != "" {
		q.Del(skipKey)
	}
	if doneText != "" {
		q.Set("done", doneText)
	}
	c.Redirect(http.StatusFound, "/admin/coupons?"+q.Encode())
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// couponRedirect 回列表页并把结论经查询参数回显（错误 ?err=、成功 ?ok=）。
func couponRedirect(c *gin.Context, okText, errText string) {
	couponRedirectSkip(c, okText, errText, "")
}

// couponRedirectSkip 同上，但先丢掉一个回跳参数（删除成功后必须丢掉 couponId）。
//
// 回跳上下文由一个隐藏域 returnQuery 整体承载，而不是 project / status / keyword 逐个铺开：
// 券的启停字段就叫 status，与筛选参数同名 —— 逐个铺开的话，
// 表单里那个 status 到底是「回跳筛选」还是「这张券的启停」只能靠猜。
func couponRedirectSkip(c *gin.Context, okText, errText, skipKey string) {
	q := url.Values{}
	if parsed, err := url.ParseQuery(strings.TrimSpace(c.PostForm("returnQuery"))); err == nil {
		// 只透传白名单键：returnQuery 同样来自客户端，不能让它往回跳 URL 里塞任意参数。
		for key, vals := range parsed {
			if _, ok := couponBackKeys[key]; ok && len(vals) > 0 {
				q.Set(key, vals[0])
			}
		}
	}
	// ok / err 一律以本次操作的结论为准，不采信客户端塞进来的提示。
	q.Del("ok")
	q.Del("err")
	if skipKey != "" {
		q.Del(skipKey)
	}
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	c.Redirect(http.StatusFound, "/admin/coupons?"+q.Encode())
}
