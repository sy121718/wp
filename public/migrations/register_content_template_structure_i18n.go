package migrations

func registerContentTemplateStructureI18n() {

	registerSeed(Seed{
		Version:   "291-content-template-structure-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.content.templates.introStructure', 'admin.content.templates.colType', 'admin.content.templates.colStatus', " +
			"'admin.content.templates.colRefs', 'admin.content.templates.activeBadge', 'admin.content.templates.activate', " +
			"'admin.content.templates.refPages', 'admin.content.templates.refInstances', 'admin.content.templates.refNone', " +
			"'admin.content.templates.refUnknown', 'admin.content.templates.entityHeader', 'admin.content.templates.entityFooter', " +
			"'admin.content.templates.roleLabel', 'admin.content.templates.noVisualEdit', " +
			"'admin.theme_settings.structure_templates', 'admin.theme_settings.header_template', " +
			"'admin.theme_settings.footer_template', 'admin.theme_settings.structure_none', " +
			"'admin.theme_settings.structure_none_footer', 'admin.theme_settings.structure_hint')",
		SQL: mustSQL("291_content_template_structure_ui_i18n.sql"),
	})
}
