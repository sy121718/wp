-- 437: Replace only exact historical mail empty-state descriptions. Titles stay unchanged.
-- The fallback in the three mail templates has the same meaning as these new values.
-- Historical 190/232 seeds use ON CONFLICT DO NOTHING, so editing them would not
-- update installed rows; operator-edited values must not be overwritten.
UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.marketing.contacts.empty', 'zh-CN', '没有匹配的联系人。', '可调整筛选条件，或在下方折叠区批量导入联系人。'),
    ('admin.mail.marketing.contacts.empty', 'en-US', 'No contacts match.', 'Adjust the filters, or import contacts in the section below.'),
    ('admin.mail.marketing.campaigns.empty', 'zh-CN', '还没有活动。新建后点「启动群发」开始发送。', '新建活动后点「启动群发」开始发送。'),
    ('admin.mail.marketing.campaigns.empty', 'en-US', 'No campaigns yet. Create one and click Start sending to begin.', 'Create a campaign, then click Start sending.'),
    ('admin.mail.campaign.links_empty', 'zh-CN', '还没有点击数据。', '启动群发后，有收件人点击邮件链接才会显示排行。'),
    ('admin.mail.campaign.links_empty', 'en-US', 'No click data yet.', 'Start sending; the ranking appears after recipients click links in the email.'),
    ('admin.mail.campaign.recipients_empty', 'zh-CN', '还没有投递记录。', '启动群发后，收件人会分批进入投递队列。'),
    ('admin.mail.campaign.recipients_empty', 'en-US', 'No delivery records yet.', 'Start sending to add recipients to the delivery queue in batches.'),
    ('admin.mail.automation.empty', 'zh-CN', '还没有流程。先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。', '先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。'),
    ('admin.mail.automation.empty', 'en-US', 'No automations yet. Create one, for example "new subscriber → wait 1 day → send the welcome email → tag them welcomed".', 'Create a flow, for example: new subscriber → wait 1 day → send a welcome email → tag them welcomed.'),
    ('admin.mail.automation.runs.empty', 'zh-CN', '还没有实例。流程启用后，满足触发条件的人会自动进入。', '流程启用后，满足触发条件的人会自动进入。'),
    ('admin.mail.automation.runs.empty', 'en-US', 'No instances yet. Once a flow is enabled, people who match the trigger enter it automatically.', 'Once a flow is enabled, people who match its trigger enter automatically.')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;
