-- 754 · 邮件退订成功提示的界面词条。
--
-- 退订成功改为提示类响应（交共享成功出口），文案由模板取词。key 按根规则：
-- 首字符字母、仅 ASCII 字母数字、1–15 字符、不含点或下划线。
--
-- 幂等：只补缺失的 (item_key, lang)，不覆盖运营已修改的译文。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('mailUnsubDone', 'zh-CN', '%s 不会再收到我们的营销邮件。', 200, 'mail', 'mail/inbound/http: 退订成功提示', 1, now(), now()),
    ('mailUnsubDone', 'en-US', '%s will no longer receive our marketing emails.', 200, 'mail', 'mail/inbound/http: 退订成功提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
