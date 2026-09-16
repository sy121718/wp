// coupon_handle.go — 后台优惠码管理页（BIZ-1 销售侧）。
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
package dashboardhttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
)

const (
	// couponPageTitle 页面标题（dashboard enums 里没有这个键，直接走 withI18n 的 fallback 链路）。
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
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	page, limit := orderListWindow(c)
	filter := couponFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		CouponID: orderQueryID(c.Query("couponId")),
	}

	// 回显文案：?err= / ?ok= 都过白名单，查不到的一律收口
	//（查询参数是用户可编辑的，不能拿它当「业务提示」直接显示）。
	pageErr := couponQueryText(c, c.Query("err"), orderInternalText(c))
	pageOk := couponQueryText(c, c.Query("ok"), "")

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

	data := withCSRF(c, gin.H{
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
		"HasDetail":         len(detail) > 0,
		"DetailCollapseURL": filterBaseURL("/admin/coupons", collapse),
		// 新建完成后回到**同一个工程**（不带 couponId：还没有展开任何券）。
		"CreateBack":      couponBackQuery(selected, filter, page, limit, 0),
		"Redemptions":     redemptions,
		"RedemptionTotal": redemptionTotal,
		"RedemptionLimit": couponRedemptionPageSize,
		"Page":            page,
		"Limit":           limit,
		"Err":             pageErr,
		"Ok":              pageOk,
	})
	base := filterBaseURL("/admin/coupons", couponFilterValues(selected, filter))
	for k, v := range buildPagination(total, page, limit, base, translateFor(c)).templateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/coupons.html", data)
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

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// couponRowView 优惠码行 → 模板视图（展示文本、编辑链接、停用表单的隐藏域都在这里定型）。
func couponRowView(cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
	if cp == nil {
		return gin.H{}
	}
	// 编辑链接在同一页面上展开（靠 couponId 参数），因此把当前窗口一起带上：
	// 收起编辑区或再翻页时，用户还站在原来那一屏。
	vals := couponFilterValues(projectID, filter)
	vals["couponId"] = strconv.FormatUint(cp.ID, 10)
	vals["page"] = strconv.Itoa(page)
	vals["limit"] = strconv.Itoa(limit)
	collapse := couponFilterValues(projectID, filter)
	collapse["page"] = strconv.Itoa(page)
	collapse["limit"] = strconv.Itoa(limit)

	return gin.H{
		"Code":             cp.Code,
		"Name":             orderTextOrEmpty(cp.Name),
		"DiscountLabel":    cp.DiscountLabel,
		"MinSubtotalLabel": cp.MinSubtotalLabel,
		"UsageLabel":       couponUsageLabel(cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"StatusLabel":      cp.StatusLabel,
		"Badge":            couponStatusBadge(cp.StatusLabel),
		"Remark":           orderTextOrEmpty(cp.Remark),
		"EditURL":          filterBaseURL("/admin/coupons", vals),
		"CollapseURL":      filterBaseURL("/admin/coupons", collapse),
		"Expanded":         filter.CouponID == cp.ID,
		// 停用 / 启用复用更新接口，表单必须回送**全部可改字段**（见 CouponUpdate 的注释）。
		"ToggleStatus": strconv.Itoa(couponToggleStatus(cp.Status)),
		"ToggleLabel":  couponToggleLabel(cp.Status),
		"Form": gin.H{
			"ID":            strconv.FormatUint(cp.ID, 10),
			"Name":          cp.Name,
			"DiscountType":  cp.DiscountType,
			"DiscountValue": cp.DiscountValue,
			"MinSubtotal":   cp.MinSubtotal,
			"MaxUses":       cp.MaxUses,
			"PerUserLimit":  cp.PerUserLimit,
			"StartsAt":      couponFormTime(cp.StartsAt.TimePtr()),
			"EndsAt":        couponFormTime(cp.EndsAt.TimePtr()),
			"Remark":        cp.Remark,
		},
		"Back": couponBackQuery(projectID, filter, page, limit, filter.CouponID),
	}
}

// couponEditView 展开区（编辑表单 + 该券的当前状态摘要）→ 模板视图。
func couponEditView(cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
	if cp == nil {
		return gin.H{}
	}
	return gin.H{
		"ID":               cp.ID,
		"Code":             cp.Code,
		"Name":             cp.Name,
		"DiscountType":     cp.DiscountType,
		"DiscountValue":    cp.DiscountValue,
		"MinSubtotal":      cp.MinSubtotal,
		"MaxUses":          cp.MaxUses,
		"PerUserLimit":     cp.PerUserLimit,
		"StartsAt":         couponFormTime(cp.StartsAt.TimePtr()),
		"EndsAt":           couponFormTime(cp.EndsAt.TimePtr()),
		"StatusValue":      strconv.Itoa(cp.Status),
		"StatusLabel":      cp.StatusLabel,
		"Badge":            couponStatusBadge(cp.StatusLabel),
		"DiscountLabel":    cp.DiscountLabel,
		"MinSubtotalLabel": cp.MinSubtotalLabel,
		"UsageLabel":       couponUsageLabel(cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"Remark":           cp.Remark,
		"Back":             couponBackQuery(projectID, filter, page, limit, filter.CouponID),
	}
}

// couponRedemptionRow 核销记录行 → 模板视图。
func couponRedemptionRow(rd *orderdto.CouponRedemptionResp) gin.H {
	if rd == nil {
		return gin.H{}
	}
	// 匿名下单（结算链路不要求先注册）没有 userId：显示「匿名访客」而不是空单元格，
	// 否则看起来像是「核销人丢了」。
	user := "匿名访客"
	if rd.UserID != nil && *rd.UserID > 0 {
		user = strconv.FormatUint(*rd.UserID, 10)
	}
	return gin.H{
		"Time":          orderTimeLabel(rd.CreateTime.Time()),
		"OrderNo":       orderTextOrEmpty(rd.OrderNo),
		"UserID":        user,
		"DiscountLabel": rd.DiscountLabel,
	}
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
		StartsAt:      strings.TrimSpace(c.PostForm("startsAt")),
		EndsAt:        strings.TrimSpace(c.PostForm("endsAt")),
		Status:        couponFormInt(c, "status"),
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorID:   couponOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	}
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

// couponOperatorID 当前登录管理员 id（写进券的 create_by / update_by）。
func couponOperatorID(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// —— 表单与文案工具 ——

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

// couponBackKeys 允许在回跳 URL 里透传的查询键（与页面 GET 认的参数一致）。
var couponBackKeys = map[string]bool{
	"project": true, "status": true, "keyword": true,
	"page": true, "limit": true, "couponId": true,
}

// couponBackQuery 当前页面的回跳查询串（写操作表单的一个隐藏域）。
func couponBackQuery(projectID string, filter couponFilter, page, limit int, couponID uint64) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("status", filter.Status)
	set("keyword", filter.Keyword)
	set("page", strconv.Itoa(page))
	set("limit", strconv.Itoa(limit))
	if couponID > 0 {
		q.Set("couponId", strconv.FormatUint(couponID, 10))
	}
	return q.Encode()
}

// couponFilterValues 列表页链接要保留的筛选条件（空值由 filterBaseURL 丢弃）。
func couponFilterValues(projectID string, filter couponFilter) map[string]string {
	return map[string]string{
		"project": projectID,
		"status":  filter.Status,
		"keyword": filter.Keyword,
	}
}

// couponFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「这个优惠码已经存在」），但它同时也可能是
// 数据库错误的原文（带表名甚至 SQL 片段）。因此先放行模块自己声明的
// orderenums.UserFacingMessages，再放行本页登记的管理侧文案，其余一律落到统一提示。
func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := couponFacingText(err.Error()); msg != "" {
		return msg
	}
	return orderInternalText(c)
}

// couponFacingText 白名单校验：命中返回原文，未命中返回空串。
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

// couponQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，
// 未命中时用 fallback（错误提示落统一文案，成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func couponQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := couponFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// couponStatusBadge 状态文案 → 徽章样式；认不出的状态给中性徽章（不猜颜色）。
func couponStatusBadge(statusLabel string) string {
	if badge, ok := couponStatusBadges[strings.TrimSpace(statusLabel)]; ok {
		return badge
	}
	return "badge-mute"
}

// couponUsageLabel 用次展示：「3 / 100」「3 / 不限」。
func couponUsageLabel(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / %s", used, couponUnlimitedLabel)
	}
	return fmt.Sprintf("%d / %d", used, max)
}

// couponNumberLabel 次数类字段展示：0 = 不限（每人限次与总上限同口径）。
func couponNumberLabel(limit int) string {
	if limit <= 0 {
		return couponUnlimitedLabel
	}
	return strconv.Itoa(limit)
}

// couponWindowLabel 时间窗展示：两端都不限时只说一次「不限」，不做「不限 ~ 不限」这种噪音。
func couponWindowLabel(startsAt, endsAt *time.Time) string {
	if startsAt == nil && endsAt == nil {
		return couponUnlimitedLabel
	}
	return couponWindowSide(startsAt) + " ~ " + couponWindowSide(endsAt)
}

// couponWindowSide 时间窗的一端：nil = 不限（不是「没有值」——
// 这里「不限」说明该侧没有约束，「—」会让人以为数据缺了）。
func couponWindowSide(at *time.Time) string {
	if at == nil {
		return couponUnlimitedLabel
	}
	return orderTimeLabel(*at)
}

// couponFormTime 时间 → 表单回填文本（空串 = 不限）。
//
// 用「2006-01-02 15:04」这一种写法而不是 <input type="datetime-local">：
// 后者提交的是带 T 的 ISO 文本，而服务端只接受 2006-01-02 / 2006-01-02 15:04(:05)，
// 带 T 的写法会被判成「生效时间不合法」—— 也就是表单自己生成的格式自己都不收。
func couponFormTime(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.Local().Format("2006-01-02 15:04")
}

// couponToggleStatus 停用 / 启用目标值：生效的券给出停用（0），其余给出启用（1）。
func couponToggleStatus(status int) int {
	if status == 1 {
		return 0
	}
	return 1
}

// couponToggleLabel 停用 / 启用按钮文案。
func couponToggleLabel(status int) string {
	if status == 1 {
		return "停用"
	}
	return "启用"
}
