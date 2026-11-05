package orderlist

import (
	"sort"
	"strings"
)

var labelFallbacks = map[string]string{
	"Loading":           "正在读取订单…",
	"Unavailable":       "订单暂不可用，请稍后重试。",
	"Empty":             "还没有订单",
	"LoginHint":         "登录后可以查看你的订单。",
	"LoginText":         "去登录",
	"Retry":             "重试",
	"All":               "全部订单",
	"Detail":            "查看详情",
	"Back":              "返回订单列表",
	"Prev":              "上一页",
	"Next":              "下一页",
	"OrderNo":           "订单号",
	"Created":           "下单时间",
	"Total":             "实付金额",
	"Subtotal":          "商品小计",
	"Discount":          "优惠",
	"Shipping":          "运费",
	"Tax":               "税费",
	"Address":           "收货信息",
	"Payment":           "付款方式",
	"Remark":            "订单备注",
	"History":           "订单进度",
	"Apply":             "申请退货",
	"ReturnTitle":       "退货申请",
	"ReturnReason":      "退货原因",
	"ReturnPlaceholder": "请说明退货原因",
	"ReturnSelect":      "选择商品",
	"Quantity":          "退货数量",
	"Returnable":        "可退数量",
	"Submit":            "提交申请",
	"Cancel":            "返回详情",
	"NoReturnable":      "这笔订单当前没有可申请退货的商品。",
	"Returns":           "售后进度",
	"ReturnNo":          "申请单号",
	"Refund":            "申请退款金额",
	"ReturnSuccess":     "退货申请已提交",
	"ReturnNotice":      "申请已受理，退款进度以售后状态为准。",
	"ScriptHint":        "请启用 JavaScript 查看订单和提交售后申请。",
	"NotFound":          "订单不存在或已无法查看。",
	"Forbidden":         "当前账号无权查看此订单。",
	"Pending":           "待付款",
	"Paid":              "已付款",
	"Shipped":           "已发货",
	"Completed":         "已完成",
	"Cancelled":         "已取消",
	"Refunded":          "已退款",
	"Requested":         "待审核",
	"Approved":          "已批准",
	"Rejected":          "已拒绝",
	"Received":          "已收货",
	"ReturnCompleted":   "已退款",
	"ReturnCancelled":   "已撤销",
}

func (v *View) ApplyI18n(t func(string, string) string) {
	if v == nil {
		return
	}
	v.Labels = make(map[string]string, len(labelFallbacks))
	for key, fallback := range labelFallbacks {
		if t != nil {
			v.Labels[key] = t("site.component.orderList."+key, fallback)
		} else {
			v.Labels[key] = fallback
		}
	}
	v.LabelAttrs = nil
	keys := make([]string, 0, len(v.Labels))
	for key := range v.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v.LabelAttrs = append(v.LabelAttrs, LabelAttribute{Key: strings.ToLower(key), Value: v.Labels[key]})
	}
	if v.Notice != "" {
		v.Notice = v.Labels["Unavailable"]
	}
}
