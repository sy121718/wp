-- Membership JSON write controls: one key, two explicitly registered languages.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.membership.write.noJs', 'zh-CN', '本页的写操作需要 JavaScript，请开启 JavaScript 后重试。', 200, 'admin', 'membership JSON write controls', 1, now(), now()),
('admin.membership.write.noJs', 'en-US', 'Writing on this page requires JavaScript. Please enable JavaScript and try again.', 200, 'admin', 'membership JSON write controls', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
