package migrations

func init() {
	registerSeed(Seed{
		Version:   "461-site-scripts-access-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 38 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +

			"'admin.settings.scripts.head_invalid', 'admin.settings.scripts.body_invalid', " +

			"'admin.settings.field.head_scripts', 'admin.settings.field.body_scripts', " +
			"'admin.settings.hint.head_scripts', 'admin.settings.hint.head_scripts.republish', " +
			"'admin.settings.hint.head_scripts.tail', 'admin.settings.hint.body_scripts', " +
			"'admin.settings.ph.head_scripts', 'admin.settings.ph.body_scripts', " +

			"'workbench.ui.settings.access', 'workbench.ui.settings.accessPublic', " +
			"'workbench.ui.settings.accessPassword', 'workbench.ui.settings.accessMembers', " +
			"'workbench.ui.settings.accessPasswordLabel', 'workbench.ui.settings.accessPasswordSet', " +
			"'workbench.ui.settings.accessPasswordUnset', 'workbench.ui.settings.accessPasswordPlaceholder', " +
			"'workbench.ui.settings.accessApply'" +
			")",
		SQL: mustSQL("461_site_scripts_access_guard_i18n.sql"),
	})
}
