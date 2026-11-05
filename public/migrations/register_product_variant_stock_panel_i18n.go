package migrations

import "sync"

func init() { registerProductVariantStockPanelI18n() }

var productVariantStockPanelI18nOnce sync.Once

func registerProductVariantStockPanelI18n() {
	productVariantStockPanelI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "660-product-variant-stock-panel-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('pStockAdd'),
    ('pStockAddLabel'),
    ('pStockTrackHint'),
    ('pStocksAddPh'),
    ('pStocksDup'),
    ('pStocksEmpty'),
    ('pStocksQty'),
    ('pStocksRemove'),
    ('pStocksToggle'),
    ('pStocksUnlimit')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
			SQL: mustSQL("723ad_current_660_product_variant_stock_panel_i18n.sql"),
		})
	})
}
