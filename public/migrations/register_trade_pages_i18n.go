package migrations

import "sync"

func registerTradePagesI18n() {
	registerTradePagesI18nOnce.Do(registerTradePagesI18nSeed)
}

var registerTradePagesI18nOnce sync.Once

func registerTradePagesI18nSeed() {

	registerSeed(Seed{
		Version:   "417-i18n-trade-pages-fix",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 16 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +

			"(item_key = 'admin.orders.list.empty_tail' AND lang = 'zh-CN' AND item_value <> '台前下单后（或后台代客建单）就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。') OR " +
			"(item_key = 'admin.orders.list.empty_tail' AND lang = 'en-US' AND item_value <> 'orders appear here once customers check out (or an admin places one on their behalf). If orders do exist, check whether the filters above are too narrow.') OR " +
			"(item_key = 'admin.customers.list.empty_heading' AND lang = 'zh-CN' AND item_value <> '没有符合条件的客户') OR " +
			"(item_key = 'admin.customers.list.empty_heading' AND lang = 'en-US' AND item_value <> 'No matching customers') OR " +
			"(item_key = 'admin.customers.list.empty_desc' AND lang = 'zh-CN' AND item_value <> '站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。') OR " +
			"(item_key = 'admin.customers.list.empty_desc' AND lang = 'en-US' AND item_value <> 'No visitor has signed up yet, or the filters are too narrow - hit Reset to see every account.') OR " +
			"(item_key = 'admin.customers.field.registered_at' AND lang = 'zh-CN' AND item_value <> '注册时间') OR " +
			"(item_key = 'admin.customers.field.registered_to' AND lang = 'zh-CN' AND item_value <> '至') OR " +

			"(item_key IN ('admin.customers.action.disable','admin.customers.action.enable'," +
			"'admin.returns.note.requested','admin.returns.note.approved','admin.returns.note.received'," +
			"'admin.returns.note.completed','admin.returns.note.rejected','admin.returns.note.cancelled') AND lang = 'en-US')",
		SQL: mustSQL("417_fix_trade_pages_i18n.sql"),
	})
}

func init() { registerTradePagesI18n() }
