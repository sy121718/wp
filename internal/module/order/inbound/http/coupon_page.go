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
//   - **写动作的结论不再走 URL，也不再走提示页**：前端直接调 JSON 接口（task-98 批次），
//     页面侧只剩「按钮 / 表单声明调用事实 + 结果区承载回执」。于是 ?err= / ?ok= / ?done= 那套读侧白名单、
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

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
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
			// 这个槽位还没取词化（coupons.html 直接渲染 LoadErrText）——
			// 事实在这里取成文本；槽位改造见 A 项遗留清单。
			loadErrText = firstNonEmpty(loadErrText, adminui.MsgText(c, couponForeignLabel))
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

	data := adminui.Prepare(c, gin.H{
		// 标题是 i18n key：adminui.Prepare 会按当前语言取词（模板不再看到裸 key）。
		"title": orderenums.CouponsHeading,
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
		// 列表上下文：拼进抽屉/表单链接的 query。**没有**回填槽（旧写法的 Echo / SubmitErr）——
		// 写路径已迁 JSON：失败时 DOM 里的输入原样保留，不需要服务端重渲片段回填。
		"ListQuery": couponListQuery(selected, filter, page, limit),
		"Page":      page,
		"Limit":     limit,
	})
	base := backpath.WithParams("/admin/coupons", map[string]string{
		"project": selected, "status": filter.Status, "keyword": filter.Keyword,
	})
	pg := pagination.Facts(total, page, limit)
	pg.Base = base
	data["Pagination"] = pg
	c.HTML(http.StatusOK, "admin/order/coupons.html", data)
}

// CouponEditForm 按需返回一张券的编辑抽屉内容（GET /admin/coupons/edit-form）。
//
// 路由复用 POST 更新权限；参数与券归属在响应片段之前校验。
// 片段只读 `.Coupon`（dto 原样）与 `.ListQuery`：写路径已迁 JSON，没有「失败重渲 + 回填」那条路，
// 所以既不需要 Echo（本次提交的原始表单），也不需要 SubmitErr（服务端算好的错误句）。
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
		adminui.PageErrorInternal(c, "coupon-edit-form", err)
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
		adminui.PageErrorInternal(c, "coupon-edit-form", err)
		return
	}
	if cp == nil || cp.ID != id || cp.ProjectID != projectID {
		c.Status(http.StatusNotFound)
		return
	}

	c.HTML(http.StatusOK, "admin/order/coupon_edit_form.html", adminui.Prepare(c, gin.H{
		"Coupon":    cp,
		"ListQuery": couponListQuery(projectID, couponFilter{Status: c.Query("status"), Keyword: c.Query("keyword")}, 0, 0),
	}))
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
	couponIDInvalidLabel.Fallback, couponForeignLabel.Fallback,
}

// 券页自造的两条文案（key + 中文兜底的事实）。
//
// 形态与 orderLabel 相同，但字段导出（adminui.Msg）—— 这两条是**页面展示槽位**的输入，
// 槽位还没取词化（见 CouponsPage 的 LoadErrText），要能直接交给 adminui.MsgText。
var (
	// couponIDInvalidLabel 表单里的券 id 不合法。
	couponIDInvalidLabel = adminui.Msg{Key: orderenums.CouponFormInvalidID, Fallback: "优惠码编号不合法，请回到列表页重新操作。"}
	// couponForeignLabel couponId 指向的是别的工程的券。
	couponForeignLabel = adminui.Msg{Key: orderenums.CouponFormForeignProject, Fallback: "这张优惠码不属于当前选中的站点工程，请切回它所属的工程再操作。"}
)

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

// —— 页面路径仍要用的取词与请求工具 ——

// couponFacingError 把 service 的错误收敛成可展示文案：命中白名单取词展示，
// 未命中记结构化日志并给归口文案（原文只进日志）。
//
// 写动作的 JSON 出口**不用**它（那里走 coupon_write.go 的 orderCouponReasonText：成品句、
// 经 pkg/response 取词，与顶层 message 同一个语言协商）；这里的消费者是列表页装载失败那条路
// （CouponsPage 的 loadErrText）与编辑抽屉：产物是按请求语言取词后的业务成品，
// 两处共用同一份白名单（couponFacingText）。
func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := couponFacingText(err.Error()); msg != "" {
		return i18n.TranslateFunc(response.RequestLanguage(c))(msg, msg)
	}
	logger.Scene("order-coupon").With("path", c.Request.URL.Path).Error(err, "优惠码写操作失败")
	return adminui.PageInternalText(c)
}

// couponEcho 本次提交的表单快照（失败片段回填用）。
//
// 走标准库的解析入口（gin 的 PostForm 也走这里），urlencoded 与 multipart 都会填进 PostForm；
// 解析错误一律忽略 —— 解析失败就当「没提交」，回填退化成空表单，提交是否合法由业务校验回答。
//
// 券的写路径已迁 JSON（不再需要回填：失败时 DOM 里的输入原样保留，见 static/js/ui/json_write.js）；
// 消费者只剩代客建单页（order_create_page.go 的失败重渲片段）—— 那条路径随 S2 迁移。
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
