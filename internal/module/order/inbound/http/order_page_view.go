package orderhttp

import (
	"strconv"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"

	"go_wp/internal/web/shell"
)

// order_page_view.go - 订单管理页的视图构造（列表/详情、状态计数与筛选选项）。

// orderListRow 订单行 → 模板视图（金额、时间、支付方式都在这里定型）。
func orderListRow(tr translate, o *orderdto.OrderResp, filter orderFilter, projectID string, page, limit int) gin.H {
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
		// ID 是批量表单里勾选框的值（字符串形态，与其它页的行 id 一致）。
		"ID":            strconv.FormatUint(o.ID, 10),
		"OrderNo":       o.OrderNo,
		"Status":        o.Status,
		"StatusLabel":   orderStatusLabel(tr, o.Status),
		"Badge":         orderStatusBadge(o.Status),
		"CustomerName":  orderTextOrEmpty(o.CustomerName),
		"CustomerEmail": orderTextOrEmpty(o.CustomerEmail),
		"TotalLabel":    orderMoneyLabel(o.Total, o.Currency),
		"PaymentLabel":  orderPaymentLabel(o.PaymentMethod, o.PaymentMethodTitle),
		"CreatedAt":     orderTimeLabel(o.CreateTime.Time()),
		"DetailURL":     shell.FilterBaseURL("/admin/orders", vals),
		"Expanded":      filter.OrderID == o.ID,
	}
}

// orderDetailView 详情（头 + 订单项 + 流转链 + 可选操作）→ 模板视图。
//
// countryLabel 把订单快照里的国家/地区代码换成当前界面语言的显示名；nil（字典未接入）
// 时原样显示代码，见 applyCountryLabel。
func orderDetailView(tr translate, d *orderdto.OrderDetailResp, filter orderFilter, projectID string, page, limit int,
	countryLabel func(string) string) gin.H {
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
			"FromLabel":         orderStatusLabel(tr, lg.FromStatus),
			"ToLabel":           orderStatusLabel(tr, lg.ToStatus),
			"OperatorTypeLabel": orderOperatorTypeLabel(tr, lg.OperatorType),
			"OperatorName":      orderTextOrEmpty(lg.OperatorName),
			"Remark":            orderTextOrEmpty(lg.Remark),
		})
	}

	transitions := make([]gin.H, 0, 2)
	for _, to := range orderNextStatuses[head.Status] {
		transitions = append(transitions, gin.H{"Value": to, "Label": orderStatusLabel(tr, to)})
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
			"StatusLabel":     orderStatusLabel(tr, head.Status),
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
			"CreatedViaLabel": orderCreatedViaLabel(tr, head.CreatedVia),
			"IPAddress":       orderTextOrEmpty(head.IPAddress),
			"UserAgent":       orderTextOrEmpty(head.UserAgent),
			"AdminNote":       orderTextOrEmpty(head.AdminNote),
			"Remark":          orderTextOrEmpty(head.Remark),
			"CancelReason":    orderTextOrEmpty(head.CancelReason),
			// 国家/地区排在最前：地址书写从大到小（国家 → 省 → 市 → 区 → 街道）。
			// 值来自快照（迁移 501 存的是代码），这里经 countryLabel 换成当前语言的名字，
			// 换不换得到都不影响订单本身的语义。
			"ShippingAddress": orderAddressLabel(applyCountryLabel(countryLabel, head.ShipCountry),
				head.ShipName, head.ShipPhone,
				head.ShipProvince, head.ShipCity, head.ShipDistrict, head.ShipAddress, head.ShipZip),
			"BillingAddress": orderAddressLabel(applyCountryLabel(countryLabel, head.BillCountry),
				head.BillName, head.BillPhone,
				head.BillProvince, head.BillCity, head.BillDistrict, head.BillAddress, head.BillZip),
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
func orderStatusCounters(tr translate, counts map[string]int64, filter orderFilter, projectID string) []gin.H {
	var all int64
	for _, n := range counts {
		all += n
	}
	out := make([]gin.H, 0, len(orderStatusViews)+1)
	out = append(out, orderStatusCounter("", orderLabelOf(tr, orderStatusAllLabel), "badge-mute", all, filter, projectID))
	for _, view := range orderStatusViews {
		out = append(out, orderStatusCounter(view.Value, orderStatusLabel(tr, view.Value), view.Badge, counts[view.Value], filter, projectID))
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
		"URL":    shell.FilterBaseURL("/admin/orders", vals),
		"Active": filter.Status == value,
	}
}

// orderBulkTargets 批量流转的目标状态候选（只有通用流转的三个）。
//
// 取消（要归还库存）与退款（要记流水号）各有独立用例，通用流转入口会拒绝它们 ——
// 放进候选只会让运营选到一个「全被跳过」的目标。
func orderBulkTargets(tr translate) []gin.H {
	options := make([]gin.H, 0, len(orderStatusViews))
	for _, view := range orderStatusViews {
		switch view.Value {
		case orderStatusCancelled, orderStatusRefunded:
			continue
		}
		options = append(options, gin.H{"Value": view.Value, "Label": orderStatusLabel(tr, view.Value)})
	}
	return options
}

// orderFilterValues 列表页链接要保留的筛选条件（空值由 shell.FilterBaseURL 丢弃）。
func orderFilterValues(projectID string, filter orderFilter) map[string]string {
	return map[string]string{
		"project":       projectID,
		"status":        filter.Status,
		"keyword":       filter.Keyword,
		"paymentMethod": filter.PaymentMethod,
	}
}

// —— 表单与文案工具 ——
