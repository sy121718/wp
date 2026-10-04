package migrations

// 522 · 邮件拆页后的 i18n 收口（定位文案修正 + 新键补种）。
//
// 与 437（只改历史精确值）同一套做法：不覆盖运营在后台改过的词条。
// 判定用「三处都已收口」而不是单条 —— 单条最容易先成功，中途失败会让半成品
// 被误判为已完成而永不修复（224 / 521 的教训）。
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
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
