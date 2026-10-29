-- 静态账号表单不再指向运行时 HTML 降级页。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('site.component.userForms.noScript', 'zh-CN', '请启用 JavaScript 以安全使用账号表单。'),
('site.component.userForms.noScript', 'en-US', 'Enable JavaScript to use account forms securely.'),
('site.component.userForms.retry', 'zh-CN', '重试'),
('site.component.userForms.retry', 'en-US', 'Retry')
ON CONFLICT (item_key, lang) DO NOTHING;
