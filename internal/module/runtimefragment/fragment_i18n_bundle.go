package runtimefragment

import (
	"fmt"

	rfenums "go_wp/internal/module/runtimefragment/enums"
)

// fragment_i18n_bundle.go — 捆绑配置器 jet 文案（I18N-012，site.fragment.bundle.*）。

type bundleConfiguratorLabels struct {
	QtyAria     string
	PricePrefix string
	Submit      string
}

func bundleConfiguratorLabelsOf(r *Request) bundleConfiguratorLabels {
	return bundleConfiguratorLabels{
		QtyAria:     r.tr(rfenums.BundleQtyAria, "数量"),
		PricePrefix: r.tr(rfenums.BundlePricePrefix, "套餐价"),
		Submit:      r.tr(rfenums.BundleSubmit, "确认数量"),
	}
}

func bundleOptionMetaLine(r *Request, minQty int, maxText string, available int) string {
	return fmt.Sprintf(r.tr(rfenums.BundleOptionMeta, "单件 %d ~ %s · 可用 %d"), minQty, maxText, available)
}

func bundlePriceLine(r *Request, price string) string {
	return bundleConfiguratorLabelsOf(r).PricePrefix + " " + price
}

func bundleResultSummaryLine(r *Request, message, totalPrice string, totalQty int) string {
	return fmt.Sprintf(r.tr(rfenums.BundleResultSummary, "%s · 套餐价 %s · 共 %d 件"), message, totalPrice, totalQty)
}

func bundleResultItemLine(r *Request, productName, sku string, qty, available int) string {
	return fmt.Sprintf(r.tr(rfenums.BundleResultItem, "%s · %s × %d（当前可用 %d）"), productName, sku, qty, available)
}
