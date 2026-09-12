// order_handle.go — 后台订单管理页（BIZ-1 销售侧）。
//
// 订单模块的后台 API 已就绪（8 个接口挂在 /api/order/* 的三层链上），但后台没有管理界面。
// 本文件补齐这个入口：一个页面同时给出工程切换、状态计数、组合筛选、订单列表、
// 详情（头 / 订单项 / 状态流转链）与三个写操作，**无 JS 也能用** ——
// 所有链接都是普通 GET，所有写操作都是原生表单 POST + csrf_token 隐藏域。
//
// 三条与订单模块的约定：
//
//  1. 跨模块只依赖 ordercontract.OrderService 与不可变 orderdto，
//     **不 import 订单模块的 model 包**：状态常量与合法流转边在这里各有一份
//     展示用的字面量表（orderStatusViews / orderNextStatuses），真值始终在服务端。
//     本页不判断流转是否合法，选错就由服务端拒绝、把它的中文错误回显到页面顶部。
//
//  2. 金额一律**整数分**（商品域是元，换算发生在商品域边界上）。
//     分 → 元的换算只在 orderAmountText 里发生一次，模板不做算术 ——
//     两处换算迟早会分叉，而订单页分叉出来的差额是要对账的。
//
//  3. 操作人（operatorType / operatorId / operatorName）由 handler 从会话覆盖写入，
//     表单里不存在这三个字段：能被客户端伪造的操作人，等于审计上没有操作人。
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
	// orderOperatorTypeAdmin 后台操作人类型（表单不传，由 handler 覆盖写入）。
	orderOperatorTypeAdmin = "admin"
	// orderFieldEmpty 空字段的展示占位：表格里的空白单元格读不出「没有值」。
	orderFieldEmpty = "—"
	// orderPageTitle 页面标题（dashboard enums 里的标题常量是那个文件的既有集合，
	// 本页面不改动它，直接用字面量走 withI18n 的 fallback 链路）。
	orderPageTitle = "订单管理"
)

// 订单状态取值（展示用字面量，刻意不 import 订单模块的 model 包）。
const (
	orderStatusPending   = "pending"
	orderStatusPaid      = "paid"
	orderStatusShipped   = "shipped"
	orderStatusCompleted = "completed"
	orderStatusCancelled = "cancelled"
	orderStatusRefunded  = "refunded"
)

// orderStatusViews 状态 → 中文标签 + 徽章样式的展示映射表。
var orderStatusViews = []struct {
	Value string
	Label string
	Badge string
}{
	{orderStatusPending, "待付款", "badge-warning"},
	{orderStatusPaid, "已付款", "badge-info"},
	{orderStatusShipped, "已发货", "badge-info"},
	{orderStatusCompleted, "已完成", "badge-success"},
	{orderStatusCancelled, "已取消", "badge-mute"},
	{orderStatusRefunded, "已退款", "badge-danger"},
}

// orderNextStatuses 通用状态流转的候选目标 —— 与订单服务端状态机的出边一致。
//
//	pending → paid；paid → shipped；shipped → completed
//
// 取消（cancelled）与退款（refunded）**不在**这里：它们各有独立用例（取消要归还库存、
// 退款要记通道流水号），服务端的通用流转入口直接拒绝这两个目标值，
// 所以它们只出现在下方各自的表单里。终态没有出边。
var orderNextStatuses = map[string][]string{
	orderStatusPending:   {orderStatusPaid},
	orderStatusPaid:      {orderStatusShipped},
	orderStatusShipped:   {orderStatusCompleted},
	orderStatusCompleted: {},
	orderStatusCancelled: {},
	orderStatusRefunded:  {},
}

// orderStatusCancellable 可取消的状态（只有未发货的单能取消：已发货的单要退就走退款，
// 退货入库另算）。这里只决定表单渲不渲染，合法性仍由服务端裁决。
var orderStatusCancellable = map[string]bool{
	orderStatusPending: true,
	orderStatusPaid:    true,
}

// orderStatusRefundable 可退款的状态（钱与货分开：退款不归还库存）。
var orderStatusRefundable = map[string]bool{
	orderStatusPaid:      true,
	orderStatusShipped:   true,
	orderStatusCompleted: true,
}

// orderPageHandle 订单管理页处理器。
type orderPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
}

// NewOrderPageHandle 构造。
func NewOrderPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService) *orderPageHandle {
	return &orderPageHandle{orders: orders, projects: projects}
}

// orderFilter 页面筛选条件（GET 参数，全部可选）。
type orderFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（订单号 / 客户邮箱 / 客户姓名，服务端决定匹配哪些列）。
	Keyword string
	// PaymentMethod 支付方式筛选（如 paypal）。
	PaymentMethod string
	// OrderID 非 0 时在列表上方渲染详情块（同一个页面，靠查询参数切换，不新开路由）。
	OrderID uint64
}

// OrdersPage 订单管理页（GET /admin/orders）。
func (h *orderPageHandle) OrdersPage(c *gin.Context) {
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
	filter := orderFilter{
		Status:        strings.TrimSpace(c.Query("status")),
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		PaymentMethod: strings.TrimSpace(c.Query("paymentMethod")),
		OrderID:       orderQueryID(c.Query("orderId")),
	}

	// 回显文案：?err= / ?ok= 都过订单模块的白名单，查不到的一律收口
	// （查询参数是用户可编辑的，不能拿它当「业务提示」直接显示）。
	pageErr := orderQueryText(c, c.Query("err"), orderInternalText(c))
	pageOk := orderQueryText(c, c.Query("ok"), "")

	rows := []gin.H{}
	counters := orderStatusCounters(nil, filter, selected)
	detail := gin.H{}
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
			pageErr = firstNonEmpty(pageErr, orderFacingError(c, lerr))
		} else {
			total = list.Total
			// 计数不受筛选影响（它回答「各状态各有多少单」这个全局问题），直接用服务端给的整份计数。
			counters = orderStatusCounters(list.Counts, filter, selected)
			for _, o := range list.List {
				rows = append(rows, orderListRow(o, filter, selected, page, limit))
			}
		}
		if filter.OrderID > 0 {
			det, derr := h.orders.GetOrder(ctx, filter.OrderID)
			if derr != nil {
				pageErr = firstNonEmpty(pageErr, orderFacingError(c, derr))
			} else {
				detail = orderDetailView(det, filter, selected, page, limit)
			}
		}
	}

	data := withCSRF(c, gin.H{
		"title":           orderPageTitle,
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		"Statuses":        counters,
		"StatusOptions":   orderStatusOptions(),
		"FilterStatus":    filter.Status,
		"FilterKeyword":   filter.Keyword,
		"FilterPayment":   filter.PaymentMethod,
		"Rows":            rows,
		"Total":           total,
		"Detail":          detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这个键决定要不要渲染详情块。
		"HasDetail": len(detail) > 0,
		"Page":      page,
		"Limit":     limit,
		"Err":       pageErr,
		"Ok":        pageOk,
	})
	base := filterBaseURL("/admin/orders", orderFilterValues(selected, filter))
	for k, v := range buildPagination(total, page, limit, base, translateFor(c)).templateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/orders.html", data)
}

// OrderStatusChange 状态流转（POST /admin/orders/status）。
func (h *orderPageHandle) OrderStatusChange(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		orderRedirect(c, "", "订单编号不合法，请回到列表页重新操作。")
		return
	}
	req := &orderdto.ChangeStatusReq{
		OrderID:  orderID,
		ToStatus: strings.TrimSpace(c.PostForm("toStatus")),
		Remark:   strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   orderOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	}
	if err := h.orders.ChangeStatus(c.Request.Context(), req); err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	orderRedirect(c, orderenums.MsgStatusChanged, "")
}

// OrderNoteSave 保存后台备注（POST /admin/orders/note）。
//
// 备注**不是状态流转**：只改 admin_note 一列，不写 status_logs —— 那条链回答的是
// 「订单处在哪一步、什么时候变过」，把备注混进去会让「这单什么时候发的货」变成要翻记录才看得出来。
func (h *orderPageHandle) OrderNoteSave(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		orderRedirect(c, "", "订单编号不合法，请回到列表页重新操作。")
		return
	}
	res, err := h.orders.UpdateOrderNote(c.Request.Context(), &orderdto.UpdateOrderNoteReq{
		OrderID:   orderID,
		AdminNote: strings.TrimSpace(c.PostForm("adminNote")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   orderOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	})
	if err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	_ = res
	orderRedirect(c, orderenums.MsgNoteUpdated, "")
}

// OrderCancel 取消订单（POST /admin/orders/cancel）：服务端会归还尚未发货那部分库存。
func (h *orderPageHandle) OrderCancel(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		orderRedirect(c, "", "订单编号不合法，请回到列表页重新操作。")
		return
	}
	req := &orderdto.CancelOrderReq{
		OrderID:      orderID,
		Reason:       strings.TrimSpace(c.PostForm("reason")),
		OperatorType: orderOperatorTypeAdmin,
		OperatorID:   orderOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	}
	if err := h.orders.CancelOrder(c.Request.Context(), req); err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	orderRedirect(c, orderenums.MsgCancelled, "")
}

// OrderRefund 退款（POST /admin/orders/refund）：**不归还库存**（退货入库是另一件事）。
func (h *orderPageHandle) OrderRefund(c *gin.Context) {
	orderID := orderQueryID(c.PostForm("orderId"))
	if orderID == 0 {
		orderRedirect(c, "", "订单编号不合法，请回到列表页重新操作。")
		return
	}
	req := &orderdto.RefundOrderReq{
		OrderID:       orderID,
		Reason:        strings.TrimSpace(c.PostForm("reason")),
		TransactionID: strings.TrimSpace(c.PostForm("transactionId")),
		OperatorType:  orderOperatorTypeAdmin,
		OperatorID:    orderOperatorID(c),
		OperatorName:  builtin.GetUsername(c),
	}
	if err := h.orders.RefundOrder(c.Request.Context(), req); err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	orderRedirect(c, orderenums.MsgRefunded, "")
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// orderListRow 订单行 → 模板视图（金额、时间、支付方式都在这里定型）。
func orderListRow(o *orderdto.OrderResp, filter orderFilter, projectID string, page, limit int) gin.H {
	if o == nil {
		return gin.H{}
	}
	vals := orderFilterValues(projectID, filter)
	// 详情链接在同一页面上展开（靠 orderId 查询参数），因此把当前窗口一起带上：
	// 关掉详情或再翻页时，用户还站在原来那一屏。
	vals["orderId"] = strconv.FormatUint(o.ID, 10)
	vals["page"] = strconv.Itoa(page)
	vals["limit"] = strconv.Itoa(limit)
	return gin.H{
		"OrderNo":       o.OrderNo,
		"Status":        o.Status,
		"StatusLabel":   orderStatusLabel(o.Status),
		"Badge":         orderStatusBadge(o.Status),
		"CustomerName":  orderTextOrEmpty(o.CustomerName),
		"CustomerEmail": orderTextOrEmpty(o.CustomerEmail),
		"TotalLabel":    orderMoneyLabel(o.Total, o.Currency),
		"PaymentLabel":  orderPaymentLabel(o.PaymentMethod, o.PaymentMethodTitle),
		"CreatedAt":     orderTimeLabel(o.CreateTime),
		"DetailURL":     filterBaseURL("/admin/orders", vals),
		"Expanded":      filter.OrderID == o.ID,
	}
}

// orderDetailView 详情（头 + 订单项 + 流转链 + 可选操作）→ 模板视图。
func orderDetailView(d *orderdto.OrderDetailResp, filter orderFilter, projectID string, page, limit int) gin.H {
	if d == nil || d.Head == nil {
		return gin.H{}
	}
	head := d.Head

	items := make([]gin.H, 0, len(d.Items))
	for _, it := range d.Items {
		if it == nil {
			continue
		}
		items = append(items, gin.H{
			"ProductName":  orderTextOrEmpty(it.ProductName),
			"VariantLabel": orderTextOrEmpty(it.VariantLabel),
			"SKU":          orderTextOrEmpty(it.SKU),
			"UnitPrice":    orderMoneyLabel(it.UnitPrice, head.Currency),
			"Quantity":     it.Quantity,
			"LineSubtotal": orderMoneyLabel(it.LineSubtotal, head.Currency),
			"LineDiscount": orderMoneyLabel(it.LineDiscount, head.Currency),
			"LineTax":      orderMoneyLabel(it.LineTax, head.Currency),
			"LineTotal":    orderMoneyLabel(it.LineTotal, head.Currency),
		})
	}

	logs := make([]gin.H, 0, len(d.Logs))
	for _, lg := range d.Logs {
		if lg == nil {
			continue
		}
		logs = append(logs, gin.H{
			"Time":              orderTimeLabel(lg.CreateTime),
			"FromLabel":         orderStatusLabel(lg.FromStatus),
			"ToLabel":           orderStatusLabel(lg.ToStatus),
			"OperatorTypeLabel": orderOperatorTypeLabel(lg.OperatorType),
			"OperatorName":      orderTextOrEmpty(lg.OperatorName),
			"Remark":            orderTextOrEmpty(lg.Remark),
		})
	}

	transitions := make([]gin.H, 0, 2)
	for _, to := range orderNextStatuses[head.Status] {
		transitions = append(transitions, gin.H{"Value": to, "Label": orderStatusLabel(to)})
	}

	// 表单回跳参数：操作完回到同一单的详情，而不是被弹回未筛选的列表第一页。
	formContext := gin.H{
		"Project":       projectID,
		"OrderID":       strconv.FormatUint(head.ID, 10),
		"Status":        filter.Status,
		"Keyword":       filter.Keyword,
		"PaymentMethod": filter.PaymentMethod,
		"Page":          strconv.Itoa(page),
		"Limit":         strconv.Itoa(limit),
	}

	return gin.H{
		"Head": gin.H{
			"ID":              head.ID,
			"OrderNo":         head.OrderNo,
			"Status":          head.Status,
			"StatusLabel":     orderStatusLabel(head.Status),
			"Badge":           orderStatusBadge(head.Status),
			"CustomerName":    orderTextOrEmpty(head.CustomerName),
			"CustomerEmail":   orderTextOrEmpty(head.CustomerEmail),
			"CustomerPhone":   orderTextOrEmpty(head.CustomerPhone),
			"Currency":        orderTextOrEmpty(head.Currency),
			"SubtotalLabel":   orderMoneyLabel(head.Subtotal, head.Currency),
			"DiscountLabel":   orderMoneyLabel(head.DiscountTotal, head.Currency),
			"ShippingLabel":   orderMoneyLabel(head.ShippingTotal, head.Currency),
			"TaxLabel":        orderMoneyLabel(head.TaxTotal, head.Currency),
			"TotalLabel":      orderMoneyLabel(head.Total, head.Currency),
			"PaymentLabel":    orderPaymentLabel(head.PaymentMethod, head.PaymentMethodTitle),
			"TransactionID":   orderTextOrEmpty(head.TransactionID),
			"PaidAt":          orderTimeLabelPtr(head.PaidAt),
			"CompletedAt":     orderTimeLabelPtr(head.CompletedAt),
			"CreatedAt":       orderTimeLabel(head.CreateTime),
			"CreatedViaLabel": orderCreatedViaLabel(head.CreatedVia),
			"IPAddress":       orderTextOrEmpty(head.IPAddress),
			"UserAgent":       orderTextOrEmpty(head.UserAgent),
			"AdminNote":       orderTextOrEmpty(head.AdminNote),
			"Remark":          orderTextOrEmpty(head.Remark),
			"CancelReason":    orderTextOrEmpty(head.CancelReason),
			"ShippingAddress": orderAddressLabel(head.ShipName, head.ShipPhone, head.ShipProvince,
				head.ShipCity, head.ShipDistrict, head.ShipAddress, head.ShipZip),
			"BillingAddress": orderAddressLabel(head.BillName, head.BillPhone, head.BillProvince,
				head.BillCity, head.BillDistrict, head.BillAddress, head.BillZip),
		},
		"Items":       items,
		"Logs":        logs,
		"Transitions": transitions,
		"CanCancel":   orderStatusCancellable[head.Status],
		"CanRefund":   orderStatusRefundable[head.Status],
		"Form":        formContext,
	}
}

// orderStatusCounters 状态计数条（「全部」+ 六个状态，各自带一个筛选链接）。
//
// counts 为 nil（未查询 / 查询失败）时全部按 0 渲染：计数条是导航，不是结论，
// 取不到数就不显示假的数字，但页面结构保持不变。
func orderStatusCounters(counts map[string]int64, filter orderFilter, projectID string) []gin.H {
	var all int64
	for _, n := range counts {
		all += n
	}
	out := make([]gin.H, 0, len(orderStatusViews)+1)
	out = append(out, orderStatusCounter("", "全部", "badge-mute", all, filter, projectID))
	for _, view := range orderStatusViews {
		out = append(out, orderStatusCounter(view.Value, view.Label, view.Badge, counts[view.Value], filter, projectID))
	}
	return out
}

// orderStatusCounter 单个计数项（带筛选链接；点「全部」即清掉 status 维度）。
func orderStatusCounter(value, label, badge string, count int64, filter orderFilter, projectID string) gin.H {
	vals := orderFilterValues(projectID, filter)
	if value == "" {
		delete(vals, "status")
	} else {
		vals["status"] = value
	}
	return gin.H{
		"Value": value, "Label": label, "Badge": badge, "Count": count,
		"URL":    filterBaseURL("/admin/orders", vals),
		"Active": filter.Status == value,
	}
}

// orderStatusOptions 状态下拉（筛选用：全部 + 六个状态）。
func orderStatusOptions() []gin.H {
	options := make([]gin.H, 0, len(orderStatusViews))
	for _, view := range orderStatusViews {
		options = append(options, gin.H{"Value": view.Value, "Label": view.Label})
	}
	return options
}

// orderFilterValues 列表页链接要保留的筛选条件（空值由 filterBaseURL 丢弃）。
func orderFilterValues(projectID string, filter orderFilter) map[string]string {
	return map[string]string{
		"project":       projectID,
		"status":        filter.Status,
		"keyword":       filter.Keyword,
		"paymentMethod": filter.PaymentMethod,
	}
}

// —— 表单与文案工具 ——

// orderRedirect 回列表页并把结论经查询参数回显（错误 ?err=、成功 ?ok=）。
//
// 表单里的隐藏域带回当前筛选与展开中的订单：一次流转之后运营看到的是
// 「同一单的新状态」，而不是被弹回未筛选的列表第一页再去重新找一遍。
func orderRedirect(c *gin.Context, okText, errText string) {
	q := url.Values{}
	for _, key := range []string{"project", "status", "keyword", "paymentMethod", "page", "limit", "orderId"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	c.Redirect(http.StatusFound, "/admin/orders?"+q.Encode())
}

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。
func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = pageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

// orderOperatorID 当前登录管理员 id（写进状态流转记录的操作人 id）。
func orderOperatorID(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// orderQueryID 解析 orderId 查询参数（非法即 0 = 不渲染详情块）。
func orderQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// orderFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderFacingText(err.Error()); msg != "" {
		return msg
	}
	return orderInternalText(c)
}

// orderFacingText 白名单校验：命中返回原文，未命中返回空串。
func orderFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range orderenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// orderQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，
// 未命中时用 fallback（错误提示落统一文案，成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func orderQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := orderFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// orderInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func orderInternalText(c *gin.Context) string {
	return translateFor(c)(dashboardenums.MsgInternalError, "系统内部错误，请稍后重试")
}

// orderStatusLabel 状态 → 中文标签（未知值原样返回：宁可显示生值，也不显示空白）。
func orderStatusLabel(status string) string {
	for _, view := range orderStatusViews {
		if view.Value == status {
			return view.Label
		}
	}
	if strings.TrimSpace(status) == "" {
		return orderFieldEmpty
	}
	return status
}

// orderStatusBadge 状态 → 徽章样式。
func orderStatusBadge(status string) string {
	for _, view := range orderStatusViews {
		if view.Value == status {
			return view.Badge
		}
	}
	return "badge-mute"
}

// orderOperatorTypeLabel 操作人类型 → 展示文案。
func orderOperatorTypeLabel(operatorType string) string {
	switch strings.TrimSpace(operatorType) {
	case "admin":
		return "后台"
	case "system":
		return "系统"
	case "customer", "user":
		return "客户"
	case "":
		return orderFieldEmpty
	default:
		return operatorType
	}
}

// orderCreatedViaLabel 下单入口 → 展示文案。
func orderCreatedViaLabel(createdVia string) string {
	switch strings.TrimSpace(createdVia) {
	case "checkout":
		return "访客结算"
	case "admin":
		return "后台自建"
	case "api":
		return "接口"
	case "":
		return orderFieldEmpty
	default:
		return createdVia
	}
}

// orderPaymentLabel 支付方式展示：通道标题优先，括号里补上通道代码（对账要看代码）。
func orderPaymentLabel(method, title string) string {
	label := strings.TrimSpace(title)
	code := strings.TrimSpace(method)
	switch {
	case label != "" && code != "":
		return label + "（" + code + "）"
	case label != "":
		return label
	case code != "":
		return code
	default:
		return orderFieldEmpty
	}
}

// orderMoneyLabel 金额（分）→ 带币种的展示文本。人民币用 ¥，其它币种用代码前缀。
func orderMoneyLabel(cents int64, currency string) string {
	amount := orderAmountText(cents)
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "CNY", "RMB":
		return "¥" + amount
	default:
		return strings.ToUpper(strings.TrimSpace(currency)) + " " + amount
	}
}

// orderAmountText 分 → 元文本（保留两位小数）。订单域金额一律整数分，
// 展示层的换算只有这一处；模板里不做任何算术。
func orderAmountText(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// orderAddressLabel 地址拼接：逐段丢掉空值（地址字段常常只填一半），
// 全空时返回空串由模板决定怎么显示。
func orderAddressLabel(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			segments = append(segments, v)
		}
	}
	return strings.Join(segments, " ")
}

// orderTimeLabel 时间 → 后台展示文本（本地时区，分钟精度）。
func orderTimeLabel(at time.Time) string {
	if at.IsZero() {
		return orderFieldEmpty
	}
	return at.Local().Format("2006-01-02 15:04")
}

// orderTimeLabelPtr 可空时间 → 展示文本。
func orderTimeLabelPtr(at *time.Time) string {
	if at == nil {
		return orderFieldEmpty
	}
	return orderTimeLabel(*at)
}

// orderTextOrEmpty 空值统一显示成「—」。
func orderTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return orderFieldEmpty
	}
	return value
}
