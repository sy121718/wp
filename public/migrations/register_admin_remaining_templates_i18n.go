package migrations

func registerAdminRemainingTemplatesI18n() {

	registerSeed(Seed{
		Version:   "284-admin-remaining-templates-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.mail.marketing.import.content.ph'),
    ('admin.mail.marketing.import.tags.ph'),
    ('admin.product_detail.template.affectedLead'),
    ('admin.product_detail.template.affectedTail'),
    ('pNewTplBindHint'),
    ('pNewTplNoneHint'),
    ('pTplAbsentHint'),
    ('pdDocumentMode'),
    ('pdPresetHint'),
    ('pdReapplyPreset'),
    ('pdTplCustomize'),
    ('pdTplDocHint'),
    ('pdTplDraftHint'),
    ('pdTplEdit'),
    ('pdTplModeTpl'),
    ('pdTplNoPubHint'),
    ('pdTplNow'),
    ('pdTplPresetEdit'),
    ('pdTplUndo'),
    ('pdTplUndoHint'),
    ('pdTplUndoSubmit')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723c_current_284_admin_remaining_templates_i18n.sql"),
	})
}
