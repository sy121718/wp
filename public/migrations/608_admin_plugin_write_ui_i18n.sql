-- 608: Plugin toggle/uninstall noscript hint and existing ErrorAuto fallback.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.plugins.write.noJs', 'zh-CN', '插件启停和卸载需要 JavaScript，请开启后重试。', 200, 'admin', 'Plugin toggle/uninstall noscript hint; upload remains available', 1, now(), now()),
('admin.plugins.write.noJs', 'en-US', 'Enabling, disabling and uninstalling plugins requires JavaScript. Enable it and try again.', 200, 'admin', 'Plugin toggle/uninstall noscript hint; upload remains available', 1, now(), now()),
('服务器内部错误，请稍后重试', 'zh-CN', '服务器内部错误，请稍后重试', 500, 'error', 'Existing pkg/response ErrorAuto fallback', 1, now(), now()),
('服务器内部错误，请稍后重试', 'en-US', 'Internal server error. Please try again later.', 500, 'error', 'Existing pkg/response ErrorAuto fallback', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
