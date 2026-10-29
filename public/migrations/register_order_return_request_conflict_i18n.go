package migrations

func init() {
	registerSeed(Seed{
		Version:      "686-order-return-request-conflict-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key = 'order.err.returnRequestConflict'",
		SQL:          mustSQL("686_order_return_request_conflict_i18n.sql"),
	})
}
