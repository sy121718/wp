package migrations

// register_stock_field_limits_i18n.go — 645 的注册项。
//
// 判据逐条枚举本批四个 key（中英成对共 8 行），不用全库总量也不用前缀 LIKE：
// 前者会被同期其它批次的行满足而静默跳过，后者会被将来新增的同前缀 key 带跑。

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
