package migrations

// 523 · 活动报表页空态动作的旧页名收口（522 漏了同一页的空态动作）。
//
// 与 437 / 522 同一套做法：只改历史精确值，不覆盖运营在后台改过的词条。
// 两语言都要到新值才算完成 —— 单语言判定会让「只改了一半」被误判为已完成。
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
func init() {
	registerSeed(Seed{
		Version:   "523-mail-campaign-action-i18n-fix",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.campaign.empty.action' AND lang = 'zh-CN' AND item_value = '回群发活动页启动')
    AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.mail.campaign.empty.action' AND lang = 'en-US' AND item_value = 'Back to campaigns to start sending')
THEN 1 ELSE 0 END`,
		SQL: mustSQL("523_mail_campaign_action_i18n_fix.sql"),
	})
}
