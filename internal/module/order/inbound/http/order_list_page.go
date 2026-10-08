package orderhttp

// order_list_page.go — 订单管理页（列表 / 详情 / 状态流转 / 取消 / 退款 / 批量）的控制器。
//
// 分工与优惠码页一致（样板见 coupon_page.go）：
//
//   - 控制器只做「绑定 → 调 service → 渲染」；文案、格式化、显示判断在模板里。
//   - **dto 直接交给模板**：Orders 是 []*orderdto.OrderResp、Detail 是 *orderdto.OrderDetailResp，
//     模板读 o.OrderNo / detail.Head.Total / it.UnitPrice 这些原始字段。Go 侧因此没有
//     orderListRow / orderDetailView / orderStatusCounters / orderBulkTargets 这一层。
//   - **写动作的结论走提示页**（shell.RenderJump），回跳地址由 shell.BackPath 从表单 action
//     的 query 里按白名单读回。于是 ?err= / ?ok= / ?done= 那套读侧白名单与
//     orderDoneTexts / orderPageDone / orderBulkActions / orderBulkExtraNotices 一起消失。
//
// 三条与状态有关的口径（模板按它们渲染，服务端仍是唯一裁决者）：
//
//  1. **通用流转的目标只有三个**：pending → paid → shipped → completed。取消与退款各有独立
//     用例（取消要归还库存、退款要记流水号），走通用入口必被拒绝，所以只出现在各自的表单里。
//  2. **可取消 = 未发货**（pending / paid）；**可退款 = 钱与货分开**（paid / shipped / completed，
//     退款不归还库存，退货入库是另一件事）。
//  3. 这三条在模板里是**显示投影**（决定按钮渲不渲染），合法性由服务端裁决 ——
//     选了不允许的边，服务端把原因经提示页回给用户。旧实现把同样的表抄在 Go 里（
//     orderNextStatuses / orderStatusCancellable / orderStatusRefundable），
//     与状态机是两份真源。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/order/enums"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
)

// orderPageHandle 订单管理页处理器。
type orderPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
	// dict 系统字典只读口：只用来把订单快照里的国家/地区代码换成当前语言的显示名。
	// 可选依赖 —— 未注入时详情照常渲染、国家显示代码（见 countryLabelFunc）：
	// 一个展示标签的字典读不到，不该让整页失败。
	dict sysconfigcontract.DictReader
}

// NewOrderPageHandle 构造。
func NewOrderPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	dict sysconfigcontract.DictReader) *orderPageHandle {
	return &orderPageHandle{orders: orders, projects: projects, dict: dict}
}

// orderFilter 页面筛选条件（GET 参数，全部可选）。
type orderFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（订单号 / 客户邮箱 / 客户姓名，服务端决定匹配哪些列）。
	Keyword string
	// PaymentMethod 支付方式筛选（如 paypal）。
	PaymentMethod string
	// OrderID 非 0 时在列表下方渲染详情块（同一个页面，靠查询参数切换，不新开路由）。
	OrderID uint64
}

// orderStatusValues 状态维度的取值白名单（含空串 = 「全部」）。
//
// 顺序 = 徽章行里的顺序；文案不在这里（真源是 orderenums.OrderStatusLabel 的
// `site.fragment.order.status.*` 词条，模板按 key 前缀取词）。
var orderStatusValues = []string{
	"", "pending", "paid", "shipped", "completed", "cancelled", "refunded",
}

// orderBulkCancelReasonRequired 批量取消缺原因时的提示（整批不处理）。
var orderBulkCancelReasonRequired = orderBulkText{orderenums.BulkCancelReasonRequired,
	"批量取消需要先填原因（表单里的备注框），本次没有处理任何订单。"}

// orderListQueryKeys 列表上下文的白名单键。
//
// 一份给「渲染时拼进表单 action」（orderListQuery），一份给「POST 回来时读回上下文」
// （shell.BackPath 的 keys 参数）—— 两处必须是同一份。**不含 status**：状态由徽章行
// 与表单各自显式拼（徽章要覆盖它、表单要保留它），混进基础串里会出现「点了别的状态徽章
// 却还带着旧状态」这种自相矛盾。
var orderListQueryKeys = []string{"project", "keyword", "paymentMethod", "page", "limit", "orderId"}

// OrdersPage 订单管理页（GET /admin/orders）。
func (h *orderPageHandle) OrdersPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
	// 页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	// 装载失败时不再去读列表与详情：工程上下文都没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查订单，查出来的是哪个工程的单都说不清。
	selected := ""
	if loadErr == nil {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	page, limit := orderListWindow(c)
	filter := orderFilter{
		Status:        strings.TrimSpace(c.Query("status")),
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		PaymentMethod: strings.TrimSpace(c.Query("paymentMethod")),
		OrderID:       orderQueryID(c.Query("orderId")),
	}

	var orders []*orderdto.OrderResp
	// 计数表一开始就补齐零值（理由同退货页：模板会拿计数做数值比较，nil 参与比较会让整页 500）。
	counts := countsFilled(nil, orderStatusValues)
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListOrders(ctx, &orderdto.ListOrderReq{
			ProjectID:     selected,
			Status:        filter.Status,
			Keyword:       filter.Keyword,
			PaymentMethod: filter.PaymentMethod,
			Offset:        (page - 1) * limit,
			Limit:         limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, lerr))
		} else {
			// 计数不受筛选影响（它回答「各状态各有多少单」这个全局问题），用服务端给的整份计数。
			orders, counts, total = list.List, countsFilled(list.Counts, orderStatusValues), list.Total
		}
	}

	var detail *orderdto.OrderDetailResp
	if selected != "" && filter.OrderID > 0 {
		det, derr := h.orders.GetOrder(ctx, &orderdto.GetOrderReq{
			ProjectID: selected, OrderID: filter.OrderID,
		})
		if derr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, derr))
		} else {
			detail = det
		}
	}

	data := shell.Prepare(c, gin.H{
		// 标题是 i18n key：shell.Prepare 按当前语言取词（模板不再看到裸 key）。
		"title":           "admin.orders.heading",
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		// 取数结果：dto 原样交给模板。
		"Orders": orders,
		"Counts": counts,
		"Total":  total,
		"Detail": detail,
		// 状态徽章行的取值白名单（含空串 = 全部）+ 当前生效值。
		"StatusValues": orderStatusValues,
		// 「全部」徽章的计数（各状态之和）：模板里跨类型累加不值得押注，求和放这里一行。
		"CountsAll": countsAll(counts),
		// 状态 → 当前语言标签（enums 的 (key, fallback) 是两返回值，模板接不住，
		// 这里收敛成单值函数；**取词仍发生在模板调用点**，Go 不预先拼行视图）。
		"OrderStatusLabel": orderStatusLabelFunc(c),
		"FilterStatus":     filter.Status,
		// 筛选回显与空态判据都由模板自己算（FilterActive 那种布尔不再由 Go 预先决定）。
		"FilterKeyword": filter.Keyword,
		"FilterPayment": filter.PaymentMethod,
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有订单」分开。
		"LoadErr": loadErrText,
		// 列表上下文（拼进表单 action 的 query；不含 status，见 orderListQueryKeys）。
		"ListQuery": orderListQuery(selected, filter, page, limit),
		// 国家/地区代码 → 当前语言的显示名。函数值直接进数据：模板里
		// `{{ .CountryLabel(code) }}` 调用它，详情不必预先翻好每一段地址。
		"CountryLabel": countryLabelFunc(c, h.dict),
		"Page":         page,
		"Limit":        limit,
	})
	base := shell.WithParams("/admin/orders", map[string]string{
		"project": selected, "status": filter.Status, "keyword": filter.Keyword,
		"paymentMethod": filter.PaymentMethod,
	})
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/order/orders.html", data)
}

// —— 写动作 ——

// OrderStatusChange 状态流转（POST /admin/orders/status）。
func (h *orderPageHandle) OrderStatusChange(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		h.orderFail(c, orderFacing(c, orderInvalidIDLabel))
		return
	}
	req := &orderdto.ChangeStatusReq{
		OrderID:  orderID,
		ToStatus: strings.TrimSpace(c.PostForm("toStatus")),
		Remark:   strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}
	if err := h.orders.ChangeStatus(c.Request.Context(), req); err != nil {
		h.orderFail(c, orderFacingError(c, err))
		return
	}
	h.orderDone(c, orderenums.MsgStatusChanged)
}

// OrderNoteSave 保存后台备注（POST /admin/orders/note）。
//
// 备注**不是状态流转**：只改 admin_note 一列，不写 status_logs —— 那条链回答的是
// 「订单处在哪一步、什么时候变过」，把备注混进去会让「这单什么时候发的货」变成要翻记录才看得出来。
func (h *orderPageHandle) OrderNoteSave(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		h.orderFail(c, orderFacing(c, orderInvalidIDLabel))
		return
	}
	if _, err := h.orders.UpdateOrderNote(c.Request.Context(), &orderdto.UpdateOrderNoteReq{
		OrderID:   orderID,
		AdminNote: strings.TrimSpace(c.PostForm("adminNote")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}); err != nil {
		h.orderFail(c, orderFacingError(c, err))
		return
	}
	h.orderDone(c, orderenums.MsgNoteUpdated)
}

// OrderCancel 取消订单（POST /admin/orders/cancel）：服务端会归还尚未发货那部分库存。
func (h *orderPageHandle) OrderCancel(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		h.orderFail(c, orderFacing(c, orderInvalidIDLabel))
		return
	}
	if _, err := h.orders.CancelOrder(c.Request.Context(), &orderdto.CancelOrderReq{
		OrderID:      orderID,
		Reason:       strings.TrimSpace(c.PostForm("reason")),
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}); err != nil {
		h.orderFail(c, orderFacingError(c, err))
		return
	}
	// 取消只有「成功 / 失败」两种结果（归还库存与释放券核销同事务，失败即回滚）。
	h.orderDone(c, orderenums.MsgCancelled)
}

// OrderRefund 退款（POST /admin/orders/refund）：**不归还库存**（退货入库是另一件事）。
func (h *orderPageHandle) OrderRefund(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		h.orderFail(c, orderFacing(c, orderInvalidIDLabel))
		return
	}
	if err := h.orders.RefundOrder(c.Request.Context(), &orderdto.RefundOrderReq{
		OrderID:       orderID,
		Reason:        strings.TrimSpace(c.PostForm("reason")),
		TransactionID: strings.TrimSpace(c.PostForm("transactionId")),
		OperatorType:  orderOperatorTypeAdmin,
		OperatorID:    shell.CurrentUserID(c),
		OperatorName:  builtin.GetUsername(c),
	}); err != nil {
		h.orderFail(c, orderFacingError(c, err))
		return
	}
	h.orderDone(c, orderenums.MsgRefunded)
}

// OrderBulkStatus 批量状态流转（POST /admin/orders/bulk-status）。
//
// 逐条走**同一条单条流转路径**：状态机不允许那条边的订单（或已不存在的单）只跳过它、
// 继续处理其余 —— 批量操作不能因为一条非法边就整批停下，否则用户会以为「一条都没做」
// 然后反复重试（重试又会在别的单上撞同一堵墙）。
func (h *orderPageHandle) OrderBulkStatus(c *gin.Context) {
	// 目标状态读 toStatus（与单条流转同名字段，也与模板批量下拉的 name 一致）。
	// 批量表单里另有一个 status，那是**回跳保留的筛选值**，两者不同义：读错这一处，
	// toStatus 恒为空串 → 整批被判「非法目标状态」→ 回执「0 个已流转」。
	toStatus := strings.TrimSpace(c.PostForm("toStatus"))
	remark := strings.TrimSpace(c.PostForm("remark"))
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.orderFail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	flowed, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		err := h.orders.ChangeStatus(c.Request.Context(), &orderdto.ChangeStatusReq{
			OrderID:      id,
			ToStatus:     toStatus,
			Remark:       remark,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if err != nil {
			skipped++
			continue
		}
		flowed++
	}
	h.orderDone(c, bulkSummary(c, orderBulkVerbFlowed, orderBulkNounOrder, flowed, skipped))
}

// OrderBulkCancel 批量取消（POST /admin/orders/bulk-cancel）。
//
// 取消原因必填：缺原因时**整批不处理**并把原因说清楚 —— 逐条取消却没有原因，
// 事后在状态流转链上会看到一串没有理由的「已取消」，那是审计上查不清的状态。
func (h *orderPageHandle) OrderBulkCancel(c *gin.Context) {
	reason := strings.TrimSpace(c.PostForm("remark"))
	if reason == "" {
		h.orderFail(c, orderBulkTextOf(c, orderBulkCancelReasonRequired))
		return
	}
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		h.orderFail(c, shell.BulkIDsFacingText(c, berr))
		return
	}
	cancelled, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		if _, err := h.orders.CancelOrder(c.Request.Context(), &orderdto.CancelOrderReq{
			OrderID:      id,
			Reason:       reason,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		}); err != nil {
			skipped++
			continue
		}
		cancelled++
	}
	h.orderDone(c, bulkSummary(c, orderBulkVerbCancelled, orderBulkNounOrder, cancelled, skipped))
}

// —— 出口与请求工具 ——

// orderDone 写成功：提示页（htmx 档由 RenderJump 换成 HX-Redirect）。
func (h *orderPageHandle) orderDone(c *gin.Context, msg string) {
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      msg,
		Back:     shell.BackPath(c, "/admin/orders", orderListQueryKeys...),
		BackText: orderBackText(c),
		Seconds:  1,
	})
}

// orderFail 写失败：提示页，不自动跳转（用户要看清原因）。
func (h *orderPageHandle) orderFail(c *gin.Context, msg string) {
	shell.RenderJump(c, shell.Jump{
		OK:       false,
		Msg:      msg,
		Back:     shell.BackPath(c, "/admin/orders", orderListQueryKeys...),
		BackText: orderBackText(c),
	})
}

// orderBackText 提示页那个链接的文字（复用页面标题词条，不新增全站词条）。
func orderBackText(c *gin.Context) string {
	return shell.TranslateFor(c)("admin.orders.heading", "订单管理")
}

// orderListQuery 列表上下文 → 查询串（拼进表单 action；空值不拼）。
//
// 刻意**不含 status**：徽章链接要覆盖它、写表单要保留它，两者各自显式拼，
// 混进基础串里会出现「点了别的状态徽章却还带着旧状态」。
func orderListQuery(projectID string, filter orderFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("keyword", filter.Keyword)
	set("paymentMethod", filter.PaymentMethod)
	if filter.OrderID > 0 {
		set("orderId", strconv.FormatUint(filter.OrderID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

// countryLabelFunc 国家/地区代码 → 当前语言的显示名（模板直接调用的函数值）。
//
// 字典未注入（装配退化）时返回一个**原样返回代码**的函数，而不是 nil ——
// 模板里 `{{ .CountryLabel(code) }}` 对 nil 函数会运行时报错、整页 500；
// 而「未接入字典」只该表现为「显示代码」（与「接入但查不到」同一条回落路径）。
func countryLabelFunc(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	label := countryLabelFn(c, dict)
	if label == nil {
		return func(code string) string { return strings.TrimSpace(code) }
	}
	return label
}

// orderFacing 取词（key + 中文兜底 → 当前语言的成品文案）。
func orderFacing(c *gin.Context, l orderLabel) string {
	return orderLabelOf(shell.TranslateFor(c), l)
}
