package migrations

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
