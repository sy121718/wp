package migrations

func init() {
	registerSeed(Seed{
		Version:   "442-i18n-inventory-moves-heading",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.inventory.moves.title' AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("442_i18n_inventory_moves_heading.sql"),
	})
}
