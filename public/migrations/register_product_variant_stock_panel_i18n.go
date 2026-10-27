package migrations

// register_product_variant_stock_panel_i18n.go — 660 的注册项（B8 仓库明细面板）。
//
// 判据逐条枚举本批 10 个 key 的 en-US 行（共 10 行门槛）。挑 en-US 而不是 zh-CN：中文行
// 与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在「本批还没跑」
// 时就成立、整批词条被静默跳过（432 / 643 的同一理由）。
//
// 不用前缀 LIKE：将来任何新增的 admin.products.stocks.* 都会把这条门槛提前满足。

import "sync"

func init() { registerProductVariantStockPanelI18n() }

var productVariantStockPanelI18nOnce sync.Once

func registerProductVariantStockPanelI18n() {
	productVariantStockPanelI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "660-product-variant-stock-panel-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang = 'en-US' AND item_key IN (" +
				"'admin.products.stocks.toggle','admin.products.stocks.qtyLabel'," +
				"'admin.products.stocks.untrackedHint','admin.products.stocks.remove'," +
				"'admin.products.stocks.duplicated','admin.products.stocks.empty'," +
				"'admin.products.stocks.add','admin.products.stocks.addLabel'," +
				"'admin.products.stocks.addPlaceholder','admin.products.stocks.unlimited')",
			SQL: mustSQL("660_product_variant_stock_panel_i18n.sql"),
		})
	})
}
