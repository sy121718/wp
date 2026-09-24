package migrations

func init() {
	registerSeed(Seed{
		Version:   "445-i18n-inventory-reason-bulk",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.inventory.bulk.reasonPartial', 'admin.inventory.bulk.reasonDone', " +
			"'admin.inventory.bulk.reasonNoneSelected') AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("445_i18n_inventory_reason_bulk.sql"),
	})
}
