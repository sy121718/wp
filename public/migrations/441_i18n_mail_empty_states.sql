-- 441: Distinguish unfiltered contact emptiness from filtered no-results;
-- update only the historical template description, preserving operator edits.
UPDATE sys_i18n AS i
SET item_value = v.new_value, update_time = now()
FROM (VALUES
    ('admin.mail.templates.empty', 'zh-CN', '还没有模板。', '新建邮件模板，填写主题与正文。'),
    ('admin.mail.templates.empty', 'en-US', 'No templates yet.', 'Create a mail template with a subject and body.')
) AS v(item_key, lang, old_value, new_value)
WHERE i.item_key = v.item_key AND i.lang = v.lang AND i.item_value = v.old_value;

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.marketing.contacts.empty.initial.title', 'zh-CN', '还没有联系人', 200, 'admin', 'admin/mail/mail_marketing.html: unfiltered contact empty title', 1, now(), now()),
('admin.mail.marketing.contacts.empty.initial.title', 'en-US', 'No contacts yet', 200, 'admin', 'admin/mail/mail_marketing.html: unfiltered contact empty title', 1, now(), now()),
('admin.mail.marketing.contacts.empty.initial', 'zh-CN', '在下方导入联系人，开始建立发送名单。', 200, 'admin', 'admin/mail/mail_marketing.html: unfiltered contact empty description', 1, now(), now()),
('admin.mail.marketing.contacts.empty.initial', 'en-US', 'Import contacts below to start building your mailing list.', 200, 'admin', 'admin/mail/mail_marketing.html: unfiltered contact empty description', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
