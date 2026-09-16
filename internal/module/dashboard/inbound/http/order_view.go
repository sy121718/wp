package dashboardhttp

import (
	"strconv"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
)

// order_view.go - 订单管理页的视图构造（列表/详情、状态计数与筛选选项）。

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
		"CreatedAt":     orderTimeLabel(o.CreateTime.Time()),
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
			"Time":              orderTimeLabel(lg.CreateTime.Time()),
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
			"PaidAt":          orderTimeLabelPtr(head.PaidAt.TimePtr()),
			"CompletedAt":     orderTimeLabelPtr(head.CompletedAt.TimePtr()),
			"CreatedAt":       orderTimeLabel(head.CreateTime.Time()),
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
