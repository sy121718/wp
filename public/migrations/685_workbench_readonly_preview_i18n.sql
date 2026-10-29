-- 统一模拟预览提示，中英成对且不覆盖作者已调整词条。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('workbench.preview.readonly', 'zh-CN', '模拟预览：不会提交真实数据，请在发布页面中操作。'),
('workbench.preview.readonly', 'en-US', 'Simulated preview: no real data is submitted. Use the published page to continue.')
ON CONFLICT (item_key, lang) DO NOTHING;
