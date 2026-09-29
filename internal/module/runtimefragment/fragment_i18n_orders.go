package runtimefragment

import (
	"fmt"

	orderenums "go_wp/internal/module/order/enums"
	rfenums "go_wp/internal/module/runtimefragment/enums"
)

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
		NeedLogin:         r.tr(rfenums.OrderNeedLoginList, "登录后可以查看你的订单。"),
		GoLogin:           userCommonLabelsOf(r).GoLogin,
		Unavailable:       r.tr(rfenums.OrderUnavailable, "订单暂不可用，请稍后再试。"),
		Empty:             r.tr(rfenums.OrderEmpty, "这里还没有订单。"),
		GoShop:            r.tr(rfenums.OrderGoShop, "去看看"),
		ViewDetail:        r.tr(rfenums.OrderViewDetail, "查看明细"),
		TabAll:            r.tr(rfenums.OrderTabAll, "全部"),
		TabsAria:          r.tr(rfenums.OrderTabsAria, "订单状态筛选"),
		PagerAria:         r.tr(rfenums.OrderPagerAria, "订单分页"),
		PrevPage:          r.tr(rfenums.OrderPrevPage, "上一页"),
		NextPage:          r.tr(rfenums.OrderNextPage, "下一页"),
		TotalCount:        r.tr(rfenums.OrderTotalCount, "共 %d 单"),
		ReaderUnavailable: r.tr(rfenums.OrderReaderUnavailable, "订单查询尚未接入"),
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
		NeedLogin:               r.tr(rfenums.OrderNeedLoginDetail, "登录后可以查看订单明细。"),
		GoLogin:                 userCommonLabelsOf(r).GoLogin,
		Unavailable:             r.tr(rfenums.OrderUnavailable, "订单暂不可用，请稍后再试。"),
		SubtotalGoods:           r.tr(rfenums.OrderSubtotalGoods, "商品合计"),
		Discount:                r.tr(rfenums.OrderDiscount, "优惠"),
		Shipping:                r.tr(rfenums.OrderShipping, "运费"),
		PayMethod:               r.tr(rfenums.OrderPayMethod, "支付方式："),
		ShippingAddr:            r.tr(rfenums.OrderShippingAddr, "收货地址："),
		Remark:                  r.tr(rfenums.OrderRemark, "备注："),
		ReturnsTitle:            r.tr(rfenums.OrderReturnsTitle, "退货申请"),
		ReturnApply:             r.tr(rfenums.OrderReturnApply, "申请退货"),
		ReturnHint:              r.tr(rfenums.OrderReturnHint, "只填要退的数量，留空表示这一行不退。提交后由客服审核，审核通过并收到货后按原支付方式退款。"),
		ReturnQtyHint:           r.tr(rfenums.OrderReturnQtyHint, "买了 %d 件，可退 %d 件"),
		ReturnQtyPlaceholder:    r.tr(rfenums.OrderReturnQtyPlaceholder, "退几件"),
		ReturnQtyAria:           r.tr(rfenums.OrderReturnQtyAria, "退货数量"),
		ReturnReason:            r.tr(rfenums.OrderReturnReason, "退货原因"),
		ReturnReasonPlaceholder: r.tr(rfenums.OrderReturnReasonPlaceholder, "例如：尺码不合适 / 收到时已破损"),
		ReturnSubmit:            r.tr(rfenums.OrderReturnSubmit, "提交退货申请"),
		NoReturnable:            r.tr(rfenums.OrderNoReturnable, "这单当前没有可退的商品。"),
		ReaderUnavailable:       r.tr(rfenums.OrderReaderUnavailable, "订单查询尚未接入"),
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
		TitleError:          r.tr(rfenums.ReturnTitleError, "提交未完成"),
		TitleSuccess:        r.tr(rfenums.ReturnTitleSuccess, "退货申请已提交"),
		StatusLine:          r.tr(rfenums.ReturnStatusLine, "申请单号 %s · 当前状态：%s"),
		RefundNote:          r.tr(rfenums.ReturnRefundNote, "预计退款 %s —— 客服审核通过并收到货后按原支付方式退回。"),
		ReasonPrefix:        r.tr(rfenums.ReturnReasonPrefix, "退货原因："),
		NeedLogin:           r.tr(rfenums.ReturnNeedLogin, "请先登录再申请退货。"),
		ProviderUnavailable: r.tr(rfenums.ReturnProviderUnavailable, "退货功能尚未接入"),
	}
}

// orderStatusLabelOf 订单状态展示名（按请求语言）。
func orderStatusLabelOf(r *Request, status string) string {
	if r == nil {
		return orderStatusFallback(status)
	}
	// 前缀取 orderenums 的既有常量（唯一真源）：该 key 前缀 `site.fragment.order.status.`
	// 由后台订单页与访客片段**共用**同一批词条（迁移 157），两处各写一份字面量会让
	// 「改一处、另一处静默留在旧前缀上」。
	key := orderenums.OrderStatusKeyPrefix + status
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
	return r.tr(rfenums.OrderCurrency, "元")
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

// fragmentUserMessage 把白名单命中的模块文案取成**当前语言**的一句（访客直接看到）。
//
// 取值两条路，按 msg 的**形态**分流：
//
//  1. 中文常量 —— 走下面的 fragmentMessageKeys 过渡映射表（「enums 还是中文常量」时代的
//     产物），命中后换成 site.fragment.msg.* 词条；
//  2. **item_key** —— cart / order 两个模块的 enums 已接 i18n，值就是 key 本身
//     （order.err.stockInsufficient / cart.err.outOfStock …）。它们进不了上面那张中文表，
//     而片段模板是**直接渲染**文本、不经过 pkg/response 的 translate —— 原先这一支原样
//     返回 msg，结果是访客在片段里看到 `order.err.stockInsufficient` 这样的裸 key。
//     按 key 直接取词即可（词条在 179/180 两批迁移里），缺词条时 fallback 给 key 本身
//     （一眼可见，不静默吞掉整句）。
//
// 判定保持「白名单已由调用方完成」：本函数只负责取词，答案的来源仍是
// cartenums/orderenums 的 UserFacingMessages（cartUserMessage / orderUserMessage）。
func fragmentUserMessage(r *Request, msg string) string {
	if r == nil || msg == "" {
		return msg
	}
	if key, ok := fragmentMessageKeys[msg]; ok {
		return r.tr(key, msg)
	}
	return r.tr(msg, msg)
}

// fragmentMessageKeys 白名单：模块 enums 中文 → site.fragment.msg.*。
var fragmentMessageKeys = map[string]string{
	"请先登录":            rfenums.FragmentMsgLoginRequired,
	"请先登录后再查看订单。":     rfenums.FragmentMsgLoginRequiredOrders,
	"订单不存在":           rfenums.FragmentMsgOrderNotFound,
	"订单不存在或无权查看。":     rfenums.FragmentMsgOrderNotFound,
	"订单暂不可用，请稍后再试。":   rfenums.FragmentMsgOrdersUnavailable,
	"购物车为空":           rfenums.FragmentMsgCartEmpty,
	"库存不足":            rfenums.FragmentMsgStockInsufficient,
	"库存不足，无法下单":       rfenums.FragmentMsgStockInsufficient,
	"商品已下架":           rfenums.FragmentMsgProductUnavailable,
	"商品规格不存在或已下架":     rfenums.FragmentMsgProductUnavailable,
	"数量无效":            rfenums.FragmentMsgQtyInvalid,
	"商品数量必须为正整数":      rfenums.FragmentMsgQtyInvalid,
	"退货申请暂不可用，请稍后再试。": rfenums.FragmentMsgReturnsUnavailable,
	"参数不合法":           rfenums.FragmentMsgInvalidParam,
	"操作失败，请稍后重试":      rfenums.FragmentMsgInternal,
	"请至少选择一件要退的商品":    rfenums.FragmentMsgReturnItemsRequired,
}
