package runtimefragment

// fragment_i18n.go — 片段访客文案取词（I18N-012，site.fragment.*）。

import (
	"fmt"

	rfenums "go_wp/internal/module/runtimefragment/enums"
)

// tr 按请求语言取 sys_i18n 词条；缺 key 时回退 fallback（与构建期组件同口径）。
func (r *Request) tr(key, fallback string) string {
	if r != nil && r.T != nil {
		return r.T(key, fallback)
	}
	return fallback
}

// cartViewLabels 购物车片段模板固定文案（由 Go 预翻译后注入 jet）。
type cartViewLabels struct {
	Empty        string
	QtyAriaLabel string
	Update       string
	TotalPrefix  string
	ItemsUnit    string
	Checkout     string
	Clear        string
}

func cartViewLabelsOf(r *Request) cartViewLabels {
	return cartViewLabels{
		Empty:        r.tr(rfenums.CartEmpty, "购物车是空的"),
		QtyAriaLabel: r.tr(rfenums.CartQtyAria, "数量"),
		Update:       r.tr(rfenums.CartUpdate, "更新"),
		TotalPrefix:  r.tr(rfenums.CartTotalPrefix, "合计"),
		ItemsUnit:    r.tr(rfenums.CartItemsUnit, "件"),
		Checkout:     r.tr(rfenums.CartCheckout, "去结算"),
		Clear:        r.tr(rfenums.CartClear, "清空购物车"),
	}
}

// checkoutViewLabels 结算结果片段固定文案。
type checkoutViewLabels struct {
	TitlePaid     string
	TitlePending  string
	OrderNoPrefix string
	TotalPrefix   string
	AccountMailed string // 含 %s 占位（邮箱）
}

func checkoutViewLabelsOf(r *Request) checkoutViewLabels {
	return checkoutViewLabels{
		TitlePaid:     r.tr(rfenums.CheckoutTitlePaid, "下单成功"),
		TitlePending:  r.tr(rfenums.CheckoutTitlePending, "订单已创建，支付未完成"),
		OrderNoPrefix: r.tr(rfenums.CheckoutOrderNo, "订单号："),
		TotalPrefix:   r.tr(rfenums.CheckoutTotal, "合计："),
		AccountMailed: r.tr(rfenums.CheckoutAccountMailed, "账号初始密码已发送至 %s，登录后可在账号中心查看订单。"),
	}
}

func availabilityMessageOf(r *Request, known bool, n int) string {
	switch {
	case !known:
		return r.tr(rfenums.StockCheckout, "以结算时库存为准")
	case n <= 0:
		return r.tr(rfenums.StockOut, "暂时缺货")
	case n <= AvailabilityLowStockThreshold:
		return fmt.Sprintf(r.tr(rfenums.StockLow, "仅剩 %d 件"), n)
	default:
		return r.tr(rfenums.StockIn, "库存充足")
	}
}
