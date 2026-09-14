package runtimefragment

import "fmt"

// fragment_i18n_orders.go — 订单 / 退货片段文案（I18N-012，site.fragment.order.*）。

// orderListLabels 订单列表。
type orderListLabels struct {
	NeedLogin         string
	GoLogin           string
	Unavailable       string
	Empty             string
	GoShop            string
	ViewDetail        string
	TabAll            string
	TabsAria          string
	PagerAria         string
	PrevPage          string
	NextPage          string
	TotalCount        string // 「共 %d 单」
	ReaderUnavailable string
}

func orderListLabelsOf(r *Request) orderListLabels {
	return orderListLabels{
		NeedLogin:         r.tr("site.fragment.order.need_login_list", "登录后可以查看你的订单。"),
		GoLogin:           userCommonLabelsOf(r).GoLogin,
		Unavailable:       r.tr("site.fragment.order.unavailable", "订单暂不可用，请稍后再试。"),
		Empty:             r.tr("site.fragment.order.empty", "这里还没有订单。"),
		GoShop:            r.tr("site.fragment.order.go_shop", "去看看"),
		ViewDetail:        r.tr("site.fragment.order.view_detail", "查看明细"),
		TabAll:            r.tr("site.fragment.order.tab_all", "全部"),
		TabsAria:          r.tr("site.fragment.order.tabs_aria", "订单状态筛选"),
		PagerAria:         r.tr("site.fragment.order.pager_aria", "订单分页"),
		PrevPage:          r.tr("site.fragment.order.prev_page", "上一页"),
		NextPage:          r.tr("site.fragment.order.next_page", "下一页"),
		TotalCount:        r.tr("site.fragment.order.total_count", "共 %d 单"),
		ReaderUnavailable: r.tr("site.fragment.order.reader_unavailable", "订单查询尚未接入"),
	}
}

// orderDetailLabels 订单详情。
type orderDetailLabels struct {
	NeedLogin               string
	GoLogin                 string
	Unavailable             string
	SubtotalGoods           string
	Discount                string
	Shipping                string
	PayMethod               string
	ShippingAddr            string
	Remark                  string
	ReturnsTitle            string
	ReturnApply             string
	ReturnHint              string
	ReturnQtyHint           string // 「买了 %d 件，可退 %d 件」
	ReturnQtyPlaceholder    string
	ReturnQtyAria           string
	ReturnReason            string
	ReturnReasonPlaceholder string
	ReturnSubmit            string
	NoReturnable            string
	ReaderUnavailable       string
}

func orderDetailLabelsOf(r *Request) orderDetailLabels {
	return orderDetailLabels{
		NeedLogin:               r.tr("site.fragment.order.need_login_detail", "登录后可以查看订单明细。"),
		GoLogin:                 userCommonLabelsOf(r).GoLogin,
		Unavailable:             r.tr("site.fragment.order.unavailable", "订单暂不可用，请稍后再试。"),
		SubtotalGoods:           r.tr("site.fragment.order.subtotal_goods", "商品合计"),
		Discount:                r.tr("site.fragment.order.discount", "优惠"),
		Shipping:                r.tr("site.fragment.order.shipping", "运费"),
		PayMethod:               r.tr("site.fragment.order.pay_method", "支付方式："),
		ShippingAddr:            r.tr("site.fragment.order.shipping_addr", "收货地址："),
		Remark:                  r.tr("site.fragment.order.remark", "备注："),
		ReturnsTitle:            r.tr("site.fragment.order.returns_title", "退货申请"),
		ReturnApply:             r.tr("site.fragment.order.return_apply", "申请退货"),
		ReturnHint:              r.tr("site.fragment.order.return_hint", "只填要退的数量，留空表示这一行不退。提交后由客服审核，审核通过并收到货后按原支付方式退款。"),
		ReturnQtyHint:           r.tr("site.fragment.order.return_qty_hint", "买了 %d 件，可退 %d 件"),
		ReturnQtyPlaceholder:    r.tr("site.fragment.order.return_qty_placeholder", "退几件"),
		ReturnQtyAria:           r.tr("site.fragment.order.return_qty_aria", "退货数量"),
		ReturnReason:            r.tr("site.fragment.order.return_reason", "退货原因"),
		ReturnReasonPlaceholder: r.tr("site.fragment.order.return_reason_placeholder", "例如：尺码不合适 / 收到时已破损"),
		ReturnSubmit:            r.tr("site.fragment.order.return_submit", "提交退货申请"),
		NoReturnable:            r.tr("site.fragment.order.no_returnable", "这单当前没有可退的商品。"),
		ReaderUnavailable:       r.tr("site.fragment.order.reader_unavailable", "订单查询尚未接入"),
	}
}

// returnResultLabels 退货申请结果。
type returnResultLabels struct {
	TitleError          string
	TitleSuccess        string
	StatusLine          string // 「申请单号 %s · 当前状态：%s」
	RefundNote          string // 「预计退款 %s —— …」
	ReasonPrefix        string
	NeedLogin           string
	ProviderUnavailable string
}

func returnResultLabelsOf(r *Request) returnResultLabels {
	return returnResultLabels{
		TitleError:          r.tr("site.fragment.return.title_error", "提交未完成"),
		TitleSuccess:        r.tr("site.fragment.return.title_success", "退货申请已提交"),
		StatusLine:          r.tr("site.fragment.return.status_line", "申请单号 %s · 当前状态：%s"),
		RefundNote:          r.tr("site.fragment.return.refund_note", "预计退款 %s —— 客服审核通过并收到货后按原支付方式退回。"),
		ReasonPrefix:        r.tr("site.fragment.return.reason_prefix", "退货原因："),
		NeedLogin:           r.tr("site.fragment.return.need_login", "请先登录再申请退货。"),
		ProviderUnavailable: r.tr("site.fragment.return.provider_unavailable", "退货功能尚未接入"),
	}
}

// orderStatusLabelOf 订单状态展示名（按请求语言）。
func orderStatusLabelOf(r *Request, status string) string {
	if r == nil {
		return orderStatusFallback(status)
	}
	key := "site.fragment.order.status." + status
	fallback := orderStatusFallback(status)
	return r.tr(key, fallback)
}

func orderStatusFallback(status string) string {
	switch status {
	case "pending":
		return "待付款"
	case "paid":
		return "已付款"
	case "shipped":
		return "已发货"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	case "refunded":
		return "已退款"
	default:
		return status
	}
}

// yuanLabel 金额单位后缀。
func yuanLabel(r *Request) string {
	if r == nil {
		return "元"
	}
	return r.tr("site.fragment.order.currency", "元")
}

// formatCentsLabel 分 → 展示串（整数除法，避免浮点展示噪声）。
func formatCentsLabel(r *Request, cents int64) string {
	neg := ""
	if cents < 0 {
		neg = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d %s", neg, cents/100, cents%100, yuanLabel(r))
}

// fragmentUserMessage 将 cart/order 模块中文常量映射为 i18n key（I18N-002 过渡层）。
func fragmentUserMessage(r *Request, msg string) string {
	if r == nil || msg == "" {
		return msg
	}
	key, ok := fragmentMessageKeys[msg]
	if !ok {
		return msg
	}
	return r.tr(key, msg)
}

// fragmentMessageKeys 白名单：模块 enums 中文 → site.fragment.msg.*。
var fragmentMessageKeys = map[string]string{
	"请先登录":            "site.fragment.msg.login_required",
	"请先登录后再查看订单。":     "site.fragment.msg.login_required_orders",
	"订单不存在":           "site.fragment.msg.order_not_found",
	"订单不存在或无权查看。":     "site.fragment.msg.order_not_found",
	"订单暂不可用，请稍后再试。":   "site.fragment.msg.orders_unavailable",
	"购物车为空":           "site.fragment.msg.cart_empty",
	"库存不足":            "site.fragment.msg.stock_insufficient",
	"库存不足，无法下单":       "site.fragment.msg.stock_insufficient",
	"商品已下架":           "site.fragment.msg.product_unavailable",
	"商品规格不存在或已下架":     "site.fragment.msg.product_unavailable",
	"数量无效":            "site.fragment.msg.qty_invalid",
	"商品数量必须为正整数":      "site.fragment.msg.qty_invalid",
	"退货申请暂不可用，请稍后再试。": "site.fragment.msg.returns_unavailable",
	"参数不合法":           "site.fragment.msg.invalid_param",
	"操作失败，请稍后重试":      "site.fragment.msg.internal",
	"请至少选择一件要退的商品":    "site.fragment.msg.return_items_required",
}
