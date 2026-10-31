package migrations

func init() {
	registerSeed(Seed{
		Version:   "522-mail-split-i18n-fixes",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.marketing.contacts.empty' AND lang = 'zh-CN' AND item_value = '可调整筛选条件，或点右上角「导入联系人」批量导入。')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.marketing.contacts.empty' AND lang = 'en-US' AND item_value = 'Adjust the filters, or use "Import contacts" at the top right to bulk import.')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.campaign.back' AND lang = 'zh-CN' AND item_value = '返回群发活动')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.campaign.back' AND lang = 'en-US' AND item_value = 'Back to campaigns')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.automation.bulk_delete_confirm' AND lang = 'zh-CN')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.automation.bulk_delete_confirm' AND lang = 'en-US')
THEN 1 ELSE 0 END`,
		SQL: mustSQL("522_mail_split_i18n_fixes.sql"),
	})
}
