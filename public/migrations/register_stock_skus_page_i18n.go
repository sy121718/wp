package migrations

// register_stock_skus_page_i18n.go — 643 的注册项。
//
// 判据逐条枚举本批 40 个 key（中英成对共 80 行），不用全库总量也不用前缀 LIKE：
// 前者会被同期其它批次的行满足而静默跳过，后者会被将来新增的同前缀 key 带跑。

import "sync"

func init() { registerStockSKUsPageI18n() }

var stockSKUsPageI18nOnce sync.Once

func registerStockSKUsPageI18n() {
	stockSKUsPageI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "643-stock-skus-page-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 80 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang IN ('zh-CN', 'en-US') AND (" +
				"item_key LIKE 'admin.inventory.stock_skus.%' OR " +
				"item_key = 'admin.inventory.filter.allStatus')",
			SQL: mustSQL("643_stock_skus_page_i18n.sql"),
		})
	})
}
