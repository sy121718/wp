package runtimefragment

import "fmt"

// fragment_i18n_bundle.go — 捆绑配置器 jet 文案（I18N-012，site.fragment.bundle.*）。

type bundleConfiguratorLabels struct {
	QtyAria     string
	PricePrefix string
	Submit      string
}

func bundleConfiguratorLabelsOf(r *Request) bundleConfiguratorLabels {
	return bundleConfiguratorLabels{
		QtyAria:     r.tr("site.fragment.bundle.qty_aria", "数量"),
		PricePrefix: r.tr("site.fragment.bundle.price_prefix", "套餐价"),
		Submit:      r.tr("site.fragment.bundle.submit", "确认数量"),
	}
}

func bundleOptionMetaLine(r *Request, minQty int, maxText string, available int) string {
	return fmt.Sprintf(r.tr("site.fragment.bundle.option_meta", "单件 %d ~ %s · 可用 %d"), minQty, maxText, available)
}

func bundlePriceLine(r *Request, price string) string {
	return bundleConfiguratorLabelsOf(r).PricePrefix + " " + price
}

func bundleResultSummaryLine(r *Request, message, totalPrice string, totalQty int) string {
	return fmt.Sprintf(r.tr("site.fragment.bundle.result_summary", "%s · 套餐价 %s · 共 %d 件"), message, totalPrice, totalQty)
}

func bundleResultItemLine(r *Request, productName, sku string, qty, available int) string {
	return fmt.Sprintf(r.tr("site.fragment.bundle.result_item", "%s · %s × %d（当前可用 %d）"), productName, sku, qty, available)
}
