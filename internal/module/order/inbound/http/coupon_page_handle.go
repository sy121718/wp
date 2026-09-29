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
	// couponRedemptionPageSize 核销记录区一次列出的条数。
	//
	// 核销记录区不做独立分页（一张券的核销次数受 MaxUses 约束，展开一屏基本看得完），
	// 超过这个数只显示最近一批，并在页面上写明「共 N 条、只显示最近 M 条」。
	// 上限同时要 ≤ 200：订单 model 对 limit 的处理是「<=0 或 >200 一律回落到 20」。
	couponRedemptionPageSize = 50
)

// 优惠码页的展示标签（key + 中文兜底，调用点 tr(key, fallback) 取词）。
var (
	// couponPageTitleLabel 页面标题（sys_i18n 已有 admin.coupons.heading）。
	couponPageTitleLabel = orderLabel{"admin.coupons.heading", "优惠码管理"}
	// couponUnlimitedLabel 「不限」口径的展示文案（次数上限 / 每人限次 / 时间窗共用）。
	//
	// 复用列表页脚注里那条现成词条（admin.coupons.list.footer.strong_unlimited = 不限）：
	// 同一个「不限」在同一页面上再灌一条同值词条，只会让翻译多一份要维护的东西。
	couponUnlimitedLabel = orderLabel{"admin.coupons.list.footer.strong_unlimited", "不限"}
	// couponIDInvalidLabel 表单里的券 id 不合法（走 ?err= 回显）。
	//
	// 写侧塞的是 **fallback（中文兜底）**：读侧白名单认的是这条中文串，
	// 取词后的译文会被自己吞掉（通道改造由共享辅助统一做，key 已备好）。
	couponIDInvalidLabel = orderLabel{"admin.coupons.form.invalid_id", "优惠码编号不合法，请回到列表页重新操作。"}
	// couponForeignLabel couponId 指向的是别的工程的券（同样走 ?err=）。
	couponForeignLabel = orderLabel{"admin.coupons.form.foreign_project", "这张优惠码不属于当前选中的站点工程，请切回它所属的工程再操作。"}
)

// couponStatusFilterValues 状态筛选下拉的取值白名单（与 CouponListReq.Status 一致）。
//
// 这四个是**展示口径**：服务端把它们翻译成时间与次数条件再下推 ——
// 过期、未开始、用尽都是时间的函数，coupons 表里并没有这些列。
// 文案不在这里：真源是 orderenums.CouponStateLabel（口径值 → 词条 key），
// 表里再存一份中文就会与它漂移。顺序 = 下拉里的顺序。
var couponStatusFilterValues = []string{
	orderenums.CouponStateEnabled,
	orderenums.CouponStateDisabled,
	orderenums.CouponStateExpired,
	orderenums.CouponStateExhausted,
}

// couponFilterStatusValue 状态筛选的**生效值**：把 URL 上的 status 归一到白名单的写法。
//
// 归一化不是洁癖，是两处实测行为逼出来的：
//
//	· service 的匹配是 strings.ToLower + TrimSpace（见 service/coupon_crud.go 的 ListCoupons），
//	  所以 ?status=ENABLED 真的按「生效中」筛过。若把 URL 原值直接交给模板，
//	  `o.Value == filterStatus` 比较失败 → 控件回显「（状态：全部）」，而列表已经是筛过的结果：
//	  控件与生效筛选自相矛盾，用户从一个说「没筛」的控件上找不到问题在哪。
//	· 不在白名单里的值不能悄悄换成空串：service 会以「参数不合法」拒绝这次筛选（页顶有提示），
//	  URL 上的值仍是用户当前的上下文。返回 ok=false 让模板把它**原样回显**出来，
//	  否则控件说「全部」、空态说「这个状态下没有优惠码」，用户既不知道筛了什么、也不知道该清什么。
func couponFilterStatusValue(raw string) (value string, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	for _, v := range couponStatusFilterValues {
		if v == s {
			return s, true
		}
	}
	return strings.TrimSpace(raw), false
}

// couponStatusBadges 状态**口径值** → 徽章样式。
//
// 键必须是口径值（enabled / not_started / …），**不能是展示文案** ——
// 这是同仓实测过的缺陷：拿服务端算好的中文 StatusLabel 当键时，运营在后台改一句词条
// （或加一条英文词条后切语言），查表就失配，徽章静默变成灰色（不报错、测试也不红）。
var couponStatusBadges = map[string]string{
	orderenums.CouponStateEnabled:    "badge-success",
	orderenums.CouponStateNotStarted: "badge-warning",
	orderenums.CouponStateExhausted:  "badge-mute",
	orderenums.CouponStateExpired:    "badge-mute",
	orderenums.CouponStateDisabled:   "badge-danger",
}

// couponTypeOptions 折扣类型下拉（与 CouponSaveReq.DiscountType 的白名单一致）。
//
// 文案按当前语言取词；返回值形状（Value / Label）与模板读法一致，模板不用改。
func couponTypeOptions(tr translate) []gin.H {
	return []gin.H{
		{"Value": "percent", "Label": orderLabelOf(tr, couponTypePercentLabel)},
		{"Value": "fixed", "Label": orderLabelOf(tr, couponTypeFixedLabel)},
	}
}

var (
	couponTypePercentLabel = orderLabel{"admin.coupons.type.percent", "按比例（折扣力度 1..100）"}
	couponTypeFixedLabel   = orderLabel{"admin.coupons.type.fixed", "固定金额（单位：分）"}
)

// couponEnableOptions 启停状态下拉（表单字段 Status：1 启用 / 0 停用）。
func couponEnableOptions(tr translate) []gin.H {
	return []gin.H{
		{"Value": "1", "Label": orderLabelOf(tr, couponFormEnabledLabel)},
		{"Value": "0", "Label": orderLabelOf(tr, couponFormDisabledLabel)},
	}
}

var (
	couponFormEnabledLabel  = orderLabel{"admin.coupons.form.enabled", "启用"}
	couponFormDisabledLabel = orderLabel{"admin.coupons.form.disabled", "停用"}
)

// couponStatusFilterOptions 状态筛选下拉项（文案按当前语言取词，形状与模板读法一致）。
func couponStatusFilterOptions(tr translate) []gin.H {
	out := make([]gin.H, 0, len(couponStatusFilterValues))
	for _, v := range couponStatusFilterValues {
		out = append(out, gin.H{"Value": v, "Label": couponStateText(tr, v)})
	}
	return out
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
	couponIDInvalidLabel.fallback, couponForeignLabel.fallback,
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
	// 展示标签的取词函数（视图组装只用它，不再在 Go 里写死中文标签）。
	tr := shell.TranslateFor(c)

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
	// 状态筛选的**生效值**：白名单内归一成规范写法（service 也按小写匹配），白名单外原样留着
	// 并把 valid 置 false —— 控件回显、空态分支与「清掉筛选」的判据都取自它。
	// 送给 service 的仍是 filter.Status 原值：非法值该被 service 拒绝并在页顶给提示，
	// 页面不在这一层替它决定「非法 = 不筛」（那会把用户手改的 URL 静默变成「看全部」）。
	filterStatusValue, filterStatusValid := couponFilterStatusValue(filter.Status)

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
				rows = append(rows, couponRowView(tr, cp, filter, selected, page, limit))
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
				pageErr = firstNonEmpty(pageErr, couponForeignLabel.fallback)
			default:
				detail = couponEditView(tr, cp, filter, selected, page, limit)
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
						redemptions = append(redemptions, couponRedemptionRow(tr, rd))
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
		"title":             orderLabelOf(tr, couponPageTitleLabel),
		"menu":              "coupons",
		"Projects":          projects,
		"SelectedProject":   selected,
		"FilterOptions":     couponStatusFilterOptions(tr),
		"TypeOptions":       couponTypeOptions(tr),
		"StatusOptions":     couponEnableOptions(tr),
		"FilterStatus":      filterStatusValue,
		"FilterStatusValid": filterStatusValid,
		"FilterKeyword":     filter.Keyword,
		"Rows":              rows,
		"Total":             total,
		"Detail":            detail,
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

// CouponEditForm 按需返回一张券的编辑抽屉（GET /admin/coupons/edit-form）。
// 路由复用 POST 更新权限；参数与券归属在响应片段之前校验。
func (h *couponPageHandle) CouponEditForm(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	rawID := c.Query("id")
	id := orderQueryID(rawID)
	projectID := strings.TrimSpace(c.Query("project"))
	if id == 0 || strconv.FormatUint(id, 10) != rawID || projectID == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	exists, err := h.projects.Exists(c.Request.Context(), projectID)
	if err != nil {
		shell.PageError(c, "coupon-edit-form", err)
		return
	}
	if !exists {
		c.Status(http.StatusNotFound)
		return
	}
	cp, err := h.orders.GetCoupon(c.Request.Context(), id)
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
	filter := couponFilter{Status: strings.TrimSpace(c.Query("status")), Keyword: strings.TrimSpace(c.Query("keyword"))}
	page, limit := orderListWindow(c)
	tr := shell.TranslateFor(c)
	row := couponRowView(tr, cp, filter, projectID, page, limit)
	form := row["Form"].(gin.H)
	back := couponBackQuery(projectID, filter, page, limit, id)
	values := gin.H{"id": form["ID"], "projectId": projectID, "returnQuery": back,
		"name": form["Name"], "discountType": form["DiscountType"], "discountValue": form["DiscountValue"],
		"minSubtotal": form["MinSubtotal"], "maxUses": form["MaxUses"], "perUserLimit": form["PerUserLimit"],
		"status": form["StatusValue"], "startsAt": form["StartsAt"], "endsAt": form["EndsAt"], "remark": form["Remark"]}
	c.HTML(http.StatusOK, "admin/order/coupon_edit_form.html", shell.Prepare(c, gin.H{
		"FormEcho": values, "EditCode": cp.Code, "TypeOptions": couponTypeOptions(tr),
		"StatusOptions": couponEnableOptions(tr),
	}))
}

// CouponCreate 新建优惠码（POST /admin/coupons/create）。
//
// 写失败不丢输入（分档契约见 coupon_form_echo.go）：htmx 提交失败时 200 + 表单片段
// （错误槽 + 回填），成功时 HX-Redirect；抽屉表单没有无 JS 提交通道，
// 原生 302 只是兜底，两条路的终点 URL 由同一份 couponEchoQuery 构造。
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
	couponRedirectWhere(c, couponRedirectTarget(c, orderenums.MsgCouponCreated))
}

// CouponUpdate 修改 / 停用 / 启用（POST /admin/coupons/update）。
//
// 停用与启用复用本方法（表单里带 status）：它们与「改门槛」「改时间窗」是同一份
// 「券的整体配置」，单独开一个状态接口只会多出一条「改状态时忘了回送门槛」的路径 ——
// UpdateCoupon 是整体更新，漏送的字段会被写成零值。
//
// 失败出口 couponEditFail 按 HX-Request 分档；停用/启用的 hidden 表单没有 hx-post，
// 走到的一律是原生档（302，行为与改造前一致）。
func (h *couponPageHandle) CouponUpdate(c *gin.Context) {
	req := couponSaveReqFromForm(c)
	if req.ID == 0 {
		h.couponEditFail(c, couponIDInvalidLabel.fallback)
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
	couponRedirectWhere(c, couponRedirectTarget(c, orderenums.MsgCouponUpdated))
}

// CouponDelete 删除优惠码（POST /admin/coupons/delete）。
//
// 有核销记录的券会被服务端拒绝（「该优惠码已有核销记录，不能删除」）：
// 删了记录就指向一张查不到的券，对账时分不清是数据坏了还是券被删了。
// 页面照原样回显服务端给的中文原因，不自己另编一套。
func (h *couponPageHandle) CouponDelete(c *gin.Context) {
	id := orderQueryID(c.PostForm("id"))
	if id == 0 {
		couponRedirect(c, "", couponIDInvalidLabel.fallback)
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
// 查询串的构造收敛在 couponEchoQuery（与成功分档出口共用同一份 URL 语义），
// 这里只保留「原生 302」这一种写出方式。
func couponRedirectSkip(c *gin.Context, okText, errText, skipKey string) {
	q := couponEchoQuery(c, okText, errText)
	if skipKey != "" {
		q.Del(skipKey)
	}
	c.Redirect(http.StatusFound, "/admin/coupons?"+q.Encode())
}
