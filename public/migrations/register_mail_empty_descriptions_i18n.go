package migrations

// The old-value guard keeps operator-edited translations intact. Once no
// historical value remains, the seed is complete even if a row was edited.
func init() {
	registerSeed(Seed{
		Version:   "437-i18n-mail-empty-descriptions",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n AS i
JOIN (VALUES
    ('admin.mail.marketing.contacts.empty', 'zh-CN', '没有匹配的联系人。'),
    ('admin.mail.marketing.contacts.empty', 'en-US', 'No contacts match.'),
    ('admin.mail.marketing.campaigns.empty', 'zh-CN', '还没有活动。新建后点「启动群发」开始发送。'),
    ('admin.mail.marketing.campaigns.empty', 'en-US', 'No campaigns yet. Create one and click Start sending to begin.'),
    ('admin.mail.campaign.links_empty', 'zh-CN', '还没有点击数据。'),
    ('admin.mail.campaign.links_empty', 'en-US', 'No click data yet.'),
    ('admin.mail.campaign.recipients_empty', 'zh-CN', '还没有投递记录。'),
    ('admin.mail.campaign.recipients_empty', 'en-US', 'No delivery records yet.'),
    ('admin.mail.automation.empty', 'zh-CN', '还没有流程。先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。'),
    ('admin.mail.automation.empty', 'en-US', 'No automations yet. Create one, for example "new subscriber → wait 1 day → send the welcome email → tag them welcomed".'),
    ('admin.mail.automation.runs.empty', 'zh-CN', '还没有实例。流程启用后，满足触发条件的人会自动进入。'),
    ('admin.mail.automation.runs.empty', 'en-US', 'No instances yet. Once a flow is enabled, people who match the trigger enter it automatically.')
) AS v(item_key, lang, old_value)
ON i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value`,
		SQL: mustSQL("437_i18n_mail_empty_descriptions.sql"),
	})
}
