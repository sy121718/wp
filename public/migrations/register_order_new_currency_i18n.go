package migrations

import "sync"

func init() { registerOrderNewCurrencyI18n() }

var orderNewCurrencyI18nOnce sync.Once

func registerOrderNewCurrencyI18n() {
	orderNewCurrencyI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "675-order-new-currency-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n
				WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (
					'admin.order_new.field.currency',
					'admin.order_new.field.currency_default',
					'admin.order_new.hint.currency',
					'order.err.currencyUnknown'
				)`,
			SQL: mustSQL("675_order_new_currency_i18n.sql"),
		})
	})
}
