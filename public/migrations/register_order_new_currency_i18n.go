package migrations

// register_order_new_currency_i18n.go — 675：代客建单页币种下拉的三个界面词条
// + 币种白名单校验的拒绝文案（order.err.currencyUnknown）。
//
// 判据逐条枚举本批的 4 个 key，并要求中英两行都在（共 8 行）：只数一种语言时另一种缺行
// 仍判已完成，那条缺失的词就永远补不上（AGENTS.md §数据库「seed 门槛必须逐条枚举本批对象」）。
// 不用前缀 LIKE：将来任何新增的 admin.order_new.* 都会让门槛提前满足。

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
