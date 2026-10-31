package migrations

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
