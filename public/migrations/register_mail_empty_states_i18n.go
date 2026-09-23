package migrations

func init() {
	registerSeed(Seed{
		Version:   "441-i18n-mail-empty-states",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN
    (SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (
        'admin.mail.marketing.contacts.empty.initial.title',
        'admin.mail.marketing.contacts.empty.initial') AND lang IN ('zh-CN', 'en-US')) = 4
    AND NOT EXISTS (
        SELECT 1 FROM sys_i18n AS i
        JOIN (VALUES
            ('admin.mail.templates.empty', 'zh-CN', '还没有模板。'),
            ('admin.mail.templates.empty', 'en-US', 'No templates yet.')
        ) AS v(item_key, lang, old_value)
        ON i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value
    ) THEN 1 ELSE 0 END`,
		SQL: mustSQL("441_i18n_mail_empty_states.sql"),
	})
}
