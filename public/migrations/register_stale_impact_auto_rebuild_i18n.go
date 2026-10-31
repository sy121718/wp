package migrations

func init() {
	registerSeed(Seed{
		Version:   "473-stale-impact-auto-rebuild-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.blocks.impact.help' " +
			"AND (item_value LIKE '%自动重建%' OR item_value LIKE '%rebuilt automatically%')",
		SQL: mustSQL("473_stale_impact_auto_rebuild_i18n.sql"),
	})
}
