INSERT INTO sys_i18n (lang, item_key, item_value) VALUES
('zh-CN', 'workbench.preview.readonly', '模拟预览：不会提交真实数据，请在发布页面中操作。'),
('en-US', 'workbench.preview.readonly', 'Preview only: no real data is submitted. Use the published page to perform actions.')
ON CONFLICT (lang, item_key) DO NOTHING;
