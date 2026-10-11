-- 611: System settings JSON write hint.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.system.write.noJs', 'zh-CN', '保存设置需要启用 JavaScript。', 200, 'admin', 'System settings JSON form', 1, now(), now()),
('admin.system.write.noJs', 'en-US', 'Enable JavaScript to save settings.', 200, 'admin', 'System settings JSON form', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
