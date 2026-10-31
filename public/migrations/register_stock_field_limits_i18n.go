package migrations

import "sync"

func init() { registerStockFieldLimitsI18n() }

var stockFieldLimitsI18nOnce sync.Once

func registerStockFieldLimitsI18n() {
	stockFieldLimitsI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "645-stock-field-limits-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
				"'admin.inventory.err.stockNameLimit', 'admin.inventory.err.stockBarcodeLimit', " +
				"'admin.inventory.err.stockRemarkLimit', 'admin.inventory.err.stockImageLimit')",
			SQL: mustSQL("645_stock_field_limits_i18n.sql"),
		})
	})
}
