package orderhttp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"

	"go_wp/internal/web/shell"
)

// return_page_view.go - 退货入库页的视图构造（列表/详情、状态计数、筛选选项与待处理提示）。

// returnListRow 退货单 → 列表行视图（状态文案、金额、时间都在这里定型）。
func returnListRow(r *orderdto.ReturnResp, filter returnFilter, projectID string, page, limit int) gin.H {
	if r == nil {
		return gin.H{}
	}
	vals := returnFilterValues(projectID, filter)
	// 详情链接在同一页面上展开（靠 returnId 查询参数），因此把当前窗口一起带上：
	// 关掉详情或再翻页时，用户还站在原来那一屏。
	vals["returnId"] = strconv.FormatUint(r.ID, 10)
	vals["page"] = strconv.Itoa(page)
	vals["limit"] = strconv.Itoa(limit)
	return gin.H{
		"ID":            r.ID,
		"ReturnNo":      r.ReturnNo,
		"OrderNo":       r.OrderNo,
		"Status":        r.Status,
		"StatusLabel":   r.StatusLabel,
		"Badge":         returnStatusBadge(r.Status),
		"CustomerName":  orderTextOrEmpty(r.CustomerName),
		"CustomerEmail": orderTextOrEmpty(r.CustomerEmail),
		"RefundLabel":   r.RefundLabel,
		"CreatedAt":     orderTimeLabel(r.CreateTime.Time()),
		"DetailURL":     shell.FilterBaseURL("/admin/returns", vals),
		"Expanded":      filter.ReturnID == r.ID,
	}
}

// returnDetailView 退货单详情（单头 + 逐行明细 + 订单摘要 + 按状态决定的操作）→ 模板视图。
func returnDetailView(d *orderdto.ReturnDetailResp, filter returnFilter, projectID string, page, limit int) gin.H {
	if d == nil || d.Return == nil {
		return gin.H{}
	}
	ret := d.Return

	items := make([]gin.H, 0, len(ret.Items))
	for _, it := range ret.Items {
		if it == nil {
			continue
		}
		items = append(items, gin.H{
			"OrderItemID":      it.OrderItemID,
			"ProductName":      orderTextOrEmpty(it.ProductName),
			"VariantLabel":     orderTextOrEmpty(it.VariantLabel),
			"SKU":              orderTextOrEmpty(it.SKU),
			"UnitPriceLabel":   it.UnitPriceLabel,
			"Quantity":         it.Quantity,
			"ReceivedQuantity": it.ReceivedQuantity,
			"RefundLabel":      it.RefundLabel,
			// Returnable 是「该订单项还能退多少」—— 运营据此判断这单还能不能再收一件退货。
			"Returnable": it.Returnable,
		})
	}

	// 订单摘要：审核时不可能不看订单（退货针对的是这一单），所以详情里一并给出。
	order := gin.H{}
	if d.Order != nil {
		o := d.Order
		order = gin.H{
			"OrderNo":       o.OrderNo,
			"OrderURL":      shell.FilterBaseURL("/admin/orders", map[string]string{"project": projectID, "orderId": strconv.FormatUint(o.ID, 10)}),
			"Status":        o.Status,
			"StatusLabel":   orderStatusLabel(o.Status),
			"Badge":         orderStatusBadge(o.Status),
			"TotalLabel":    orderMoneyLabel(o.Total, o.Currency),
			"CreatedAt":     orderTimeLabel(o.CreateTime.Time()),
			"CustomerName":  orderTextOrEmpty(o.CustomerName),
			"CustomerEmail": orderTextOrEmpty(o.CustomerEmail),
			"ShipName":      orderTextOrEmpty(o.ShipName),
			"ShipPhone":     orderTextOrEmpty(o.ShipPhone),
			"ShipAddress": orderAddressLabel(o.ShipProvince, o.ShipCity, o.ShipDistrict,
				o.ShipAddress, o.ShipZip),
		}
	}

	// 表单回跳参数：操作完回到同一张退货单的详情，而不是被弹回未筛选的列表第一页。
	form := gin.H{
		"Project":  projectID,
		"OrderID":  returnOrderIDText(filter.OrderID),
		"ReturnID": strconv.FormatUint(ret.ID, 10),
		"Status":   filter.Status,
		"Keyword":  filter.Keyword,
		"Page":     strconv.Itoa(page),
		"Limit":    strconv.Itoa(limit),
	}

	// 操作开关按状态给：**不渲染当前状态不可能成功的按钮**（合法性仍由服务端裁决）。
	// requested 才有「同意 / 拒绝」；approved 是「确认收货并退货」；
	// received 只补退款 —— 同一入口，语义不同。
	requested := ret.Status == returnStatusRequested
	approved := ret.Status == returnStatusApproved
	received := ret.Status == returnStatusReceived

	// 状态说明（key + 中文兜底）：模板把当前状态那一句渲染进「操作」标题的 .help 悬浮，
	// 不再平铺在正文里 —— 它是操作指引，不是数据；每行/每屏重复一遍只会盖住真正的表单。
	noteKey, noteText := returnStatusNote(ret.Status)

	return gin.H{
		"Head": gin.H{
			"ID":            ret.ID,
			"ReturnNo":      ret.ReturnNo,
			"OrderNo":       ret.OrderNo,
			"OrderURL":      shell.FilterBaseURL("/admin/orders", map[string]string{"project": projectID, "orderId": strconv.FormatUint(ret.OrderID, 10)}),
			"Status":        ret.Status,
			"StatusLabel":   ret.StatusLabel,
			"Badge":         returnStatusBadge(ret.Status),
			"Reason":        orderTextOrEmpty(ret.Reason),
			"RefundLabel":   ret.RefundLabel,
			"CustomerName":  orderTextOrEmpty(ret.CustomerName),
			"CustomerEmail": orderTextOrEmpty(ret.CustomerEmail),
			"AdminNote":     orderTextOrEmpty(ret.AdminNote),
			"ReviewerName":  orderTextOrEmpty(ret.ReviewerName),
			"ReviewedAt":    orderTimeLabelPtr(ret.ReviewedAt.TimePtr()),
			"ReceivedAt":    orderTimeLabelPtr(ret.ReceivedAt.TimePtr()),
			"RefundedAt":    orderTimeLabelPtr(ret.RefundedAt.TimePtr()),
			"TransactionID": orderTextOrEmpty(ret.TransactionID),
			"CreatedAt":     orderTimeLabel(ret.CreateTime.Time()),
			"UpdatedAt":     orderTimeLabel(ret.UpdateTime.Time()),
		},
		"Items":          items,
		"Order":          order,
		"HasOrder":       len(order) > 0,
		"NoteKey":        noteKey,
		"Note":           noteText,
		"CanApprove":     requested,
		"CanReject":      requested,
		"CanReceive":     approved,
		"CanRetryRefund": received,
		"Form":           form,
	}
}

// returnStatusCounters 状态计数条（「全部」+ 六个状态，各自带一个筛选链接）。
//
// counts 为 nil（未查询 / 查询失败）时全部按 0 渲染：计数条是导航，不是结论，
// 取不到数就不显示假的数字，但页面结构保持不变。
func returnStatusCounters(counts map[string]int64, filter returnFilter, projectID string) []gin.H {
	var all int64
	for _, n := range counts {
		all += n
	}
	out := make([]gin.H, 0, len(returnStatusViews)+1)
	out = append(out, returnStatusCounter("", "全部", "badge-mute", false, all, filter, projectID))
	for _, view := range returnStatusViews {
		out = append(out, returnStatusCounter(view.Value, view.Label, view.Badge, view.Highlight,
			counts[view.Value], filter, projectID))
	}
	return out
}

// returnStatusCounter 单个计数项（带筛选链接；点「全部」即清掉 status 维度）。
func returnStatusCounter(value, label, badge string, highlight bool, count int64, filter returnFilter, projectID string) gin.H {
	vals := returnFilterValues(projectID, filter)
	if value == "" {
		delete(vals, "status")
	} else {
		vals["status"] = value
	}
	return gin.H{
		"Value": value, "Label": label, "Badge": badge, "Count": count, "Highlight": highlight,
		"URL":    shell.FilterBaseURL("/admin/returns", vals),
		"Active": filter.Status == value,
	}
}

// returnFilterValues 列表页链接要保留的筛选条件（空值由 shell.FilterBaseURL 丢弃）。
//
// 这里**不含 returnId**：点状态、翻页这些动作的语义是「换一批单看」，
// 顺手把详情展开态带过去只会让人以为页面坏了。展开态由行内链接单独加。
func returnFilterValues(projectID string, filter returnFilter) map[string]string {
	vals := map[string]string{
		"project": projectID,
		"status":  filter.Status,
		"keyword": filter.Keyword,
	}
	if filter.OrderID > 0 {
		vals["orderId"] = strconv.FormatUint(filter.OrderID, 10)
	}
	return vals
}

// returnPendingHint 顶部待办提示：把「有人在等」的两类数字单独拎出来说一句。
// 两类都为 0 时返回空串，模板据此不渲染。
func returnPendingHint(counters []gin.H) string {
	var pending, refund int64
	for _, item := range counters {
		switch item["Value"] {
		case returnStatusRequested:
			pending, _ = item["Count"].(int64)
		case returnStatusReceived:
			refund, _ = item["Count"].(int64)
		}
	}
	if pending == 0 && refund == 0 {
		return ""
	}
	return fmt.Sprintf("当前有 %d 单待审核、%d 单已入库待退款 —— 这两个状态是有人在等着处理的。", pending, refund)
}

// —— 表单与文案工具 ——

// returnWarehouseOption 入库仓库下拉项（value 是仓库 id，Label 带短码与默认标记）。
type returnWarehouseOption struct {
	ID    string
	Label string
}

// warehouseOptions 某工程的仓库下拉项（默认仓在最前，由 inventory 侧排序保证）。
//
// 取不到时返回空表：页面据此只渲染「默认仓」一个选项，而不是让整页报错 ——
// 退货审核本身不依赖仓库列表（服务端在仓库为空时会兜底到默认仓）。
func (h *returnPageHandle) warehouseOptions(ctx context.Context, projectID string) []returnWarehouseOption {
	if h.warehouses == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	list, err := h.warehouses.ListReturnWarehouses(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]returnWarehouseOption, 0, len(list))
	for _, w := range list {
		if opt, ok := warehouseOptionOf(&w); ok {
			out = append(out, opt)
		}
	}
	return out
}

// warehouseOptionOf 仓库 → 下拉项（纯函数：IO 与文案拼装分开，口径可单测）。
//
// 三条取舍：
//
//	· **停用的仓不进下拉**（历史流水仍按 id 可读，但新入库不该再选它）；
//	· 标签带短码 —— 短码是仓库在 SKU 编码里的前缀，运营认得出「SZ」比认全名快；
//	· 默认仓显式标注 —— 留空时的兜底目标要让运营看得见，否则「我什么都没选」与
//	  「我以为会进某个仓」之间就靠猜。
func warehouseOptionOf(w *ordercontract.ReturnWarehouse) (opt returnWarehouseOption, ok bool) {
	if w == nil || strings.TrimSpace(w.ID) == "" {
		return opt, false
	}
	if w.Status != "" && w.Status != "enabled" {
		return opt, false
	}
	label := strings.TrimSpace(w.Name)
	if code := strings.TrimSpace(w.Code); code != "" {
		if label == "" {
			label = code
		} else {
			label = label + "（" + code + "）"
		}
	}
	if w.IsDefault {
		label += " · 默认仓"
	}
	return returnWarehouseOption{ID: w.ID, Label: label}, true
}
