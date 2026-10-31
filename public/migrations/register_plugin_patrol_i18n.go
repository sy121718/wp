package migrations

func registerPluginPatrolI18n() {

	registerSeed(Seed{
		Version:   "279-plugin-patrol-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.plugins.patrol.orphans_title', 'admin.plugins.patrol.orphans_desc', " +
			"'admin.plugins.patrol.col_tables', 'admin.plugins.patrol.col_hint', " +
			"'admin.plugins.patrol.hint_empty', 'admin.plugins.patrol.hint_filled', " +
			"'admin.plugins.patrol.missing_title', 'admin.plugins.patrol.missing_desc', " +
			"'admin.plugins.patrol.orphan_storage_title', 'admin.plugins.patrol.orphan_storage_desc', " +
			"'admin.plugins.patrol.missing_storage_title', 'admin.plugins.patrol.missing_storage_desc')",
		SQL: mustSQL("279_plugin_patrol_i18n.sql"),
	})
}
