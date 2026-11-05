package migrations

func registerAdminDataruleEditorI18n() {
	registerSeed(Seed{
		Version:      "707-admin-datarule-editor-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('admin.datarules.editor.domain_conflict', 'admin.datarules.editor.load_failed', 'admin.datarules.editor.loading')",
		SQL:          mustSQL("707_admin_datarule_editor_i18n.sql"),
	})
}
