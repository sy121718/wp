package orderhttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/web/shell"
)

// order_page_handle.go — 后台订单管理页（BIZ-1 销售侧）。

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

const (
	// orderOperatorTypeAdmin 后台操作人类型（表单不传，由 handler 覆盖写入）。
	orderOperatorTypeAdmin = "admin"
	// orderFieldEmpty 空字段的展示占位：表格里的空白单元格读不出「没有值」。
	orderFieldEmpty = "—"
	// orderPageTitle 页面标题（订单模块 enums 里没有这个标题键，
	// 本页面不改动它，直接用字面量走 shell.Prepare 的 fallback 链路）。
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
		c.String(http.StatusInternalServerError, orderenums.ErrInternal)
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
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), orderFacingText)
	pageOk := shell.FacingQueryText(c.Query("ok"), "", orderFacingText)

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
			det, derr := h.orders.GetOrder(ctx, &orderdto.GetOrderReq{
				ProjectID: selected, OrderID: filter.OrderID,
			})
			if derr != nil {
				pageErr = firstNonEmpty(pageErr, orderFacingError(c, derr))
			} else {
				detail = orderDetailView(det, filter, selected, page, limit)
			}
		}
	}

	data := shell.Prepare(c, gin.H{
		"title":           orderPageTitle,
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		"Statuses":        counters,
		// 状态维度**只有**计数徽章一种入口：徽章行已经能点，再摆一个同维度的下拉，
		// 用户会怀疑两者是否等价（admin-ui-logic §3「同一维度只给一种筛选控件」）。
		"BulkTargets":   orderBulkTargets(),
		"FilterStatus":  filter.Status,
		"FilterKeyword": filter.Keyword,
		"FilterPayment": filter.PaymentMethod,
		"Rows":          rows,
		"Total":         total,
		"Detail":        detail,
		// 显式布尔：Jet 对空 map 的真值判断不值得押注，页面靠这个键决定要不要渲染详情块。
		"HasDetail": len(detail) > 0,
		"Page":      page,
		"Limit":     limit,
		"Err":       pageErr,
		"Ok":        pageOk,
		// 批量动作的结论：数量是动态的，过不了 ?ok= / ?err= 的文案白名单，单独走 ?done=。
		"Done": orderPageDone(c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/orders", orderFilterValues(selected, filter))
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
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
		OperatorID:   shell.CurrentUserID(c),
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
		OperatorID:   shell.CurrentUserID(c),
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
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}
	if _, err := h.orders.CancelOrder(c.Request.Context(), req); err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	// 取消只有「成功 / 失败」两种结果（归还库存与释放券核销同事务，失败即回滚），
	// 历史遗留的「带警告成功」出口已随事务收口删除。
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
		OperatorID:    shell.CurrentUserID(c),
		OperatorName:  builtin.GetUsername(c),
	}
	if err := h.orders.RefundOrder(c.Request.Context(), req); err != nil {
		orderRedirect(c, "", orderFacingError(c, err))
		return
	}
	orderRedirect(c, orderenums.MsgRefunded, "")
}

// OrderBulkStatus 批量状态流转（POST /admin/orders/bulk-status）。
//
// 逐条走**同一条单条流转路径**：状态机不允许那条边的订单（或已不存在的单）只跳过它、
// 继续处理其余 —— 批量操作不能因为一条非法边就整批停下，否则用户会以为「一条都没做」
// 然后反复重试（重试又会在别的单上撞同一堵墙）。
//
// 目标状态只列通用流转的三个：取消与退款各有独立用例（取消要归还库存、退款要记流水号），
// 走通用入口必被服务端拒绝，放进候选只会制造「选了一个全被跳过的目标」。
func (h *orderPageHandle) OrderBulkStatus(c *gin.Context) {
	toStatus := strings.TrimSpace(c.PostForm("status"))
	remark := strings.TrimSpace(c.PostForm("remark"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		orderRedirect(c, "", orderBulkIDsText(c, berr))
		return
	}
	changed, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		err := h.orders.ChangeStatus(c.Request.Context(), &orderdto.ChangeStatusReq{
			OrderID:  id,
			ToStatus: toStatus,
			Remark:   remark,
			// 操作人由会话覆盖写入，绝不受表单影响。
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if err != nil {
			skipped++
			continue
		}
		changed++
	}
	orderBulkRedirect(c, bulkSummary("已流转", "订单", changed, skipped))
}

// OrderBulkCancel 批量取消订单（POST /admin/orders/bulk-cancel）。
//
// 取消会归还尚未发货那部分库存，因此与单条共用 CancelOrder；已发货 / 已完成的单
// 由服务端状态机拒绝，计入跳过而不中断整批。
//
// 原因**必填**：单条取消也要求它，而「批量取消没写原因」会让状态流转链上留下一批
// 说不清理由的记录。缺原因时整批不处理并原样回带一句提示 —— 静默跳过会让人以为
// 「选中的单都不可取消」。
func (h *orderPageHandle) OrderBulkCancel(c *gin.Context) {
	reason := strings.TrimSpace(c.PostForm("remark"))
	if reason == "" {
		orderBulkRedirect(c, "批量取消需要先填原因（表单里的备注框），本次没有处理任何订单。")
		return
	}
	projectID := strings.TrimSpace(c.PostForm("project"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		orderRedirect(c, "", orderBulkIDsText(c, berr))
		return
	}
	cancelled, skipped := 0, 0
	for _, raw := range ids {
		id := orderQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		_, err := h.orders.CancelOrder(c.Request.Context(), &orderdto.CancelOrderReq{
			OrderID: id,
			Reason:  reason,
			// 工程作用域（DB-009）：orders 带 FORCE 策略，缺作用域时下面的三步会静默落空。
			ProjectID:    projectID,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   shell.CurrentUserID(c),
			OperatorName: builtin.GetUsername(c),
		})
		if err != nil {
			skipped++
			continue
		}
		cancelled++
	}
	orderBulkRedirect(c, bulkSummary("已取消", "订单", cancelled, skipped))
}

// bulkSummary 批量动作的结果文案（成功 N 个 / 跳过 M 个）；订单与退货申请共用。
//
// 「跳过」必须出现在文案里：只报成功数会让「选了 10 个、实际改了 6 个」看起来像全做完了，
// 而剩下的那几个会在下次列表刷新时莫名其妙地回到原状。
func bulkSummary(verb, noun string, done, skipped int) string {
	switch {
	case done == 0 && skipped == 0:
		return fmt.Sprintf(orderBulkNothingSelected, noun)
	case skipped == 0:
		return fmt.Sprintf(orderBulkAllDone, verb, done, noun)
	case done == 0:
		return fmt.Sprintf(orderBulkAllSkipped, noun, verb, skipped)
	default:
		return fmt.Sprintf(orderBulkPartial, verb, done, noun, skipped)
	}
}

// orderBulkRedirect 批量动作回列表页：结论走 ?done=，当前筛选与窗口原样带回。
//
// 不带 orderId：批量是列表级动作，把某一单的展开态带回来只会让人以为批量改的是那一单。
func orderBulkRedirect(c *gin.Context, doneText string) {
	q := url.Values{}
	for _, key := range []string{"project", "status", "keyword", "paymentMethod", "page", "limit"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	if doneText != "" {
		q.Set("done", doneText)
	}
	c.Redirect(http.StatusFound, "/admin/orders?"+q.Encode())
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

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
