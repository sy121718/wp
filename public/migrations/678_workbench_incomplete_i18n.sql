-- 草稿中的未完成控件局部占位。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('workbench.preview.incomplete', 'zh-CN', '待完善内容'),
('workbench.preview.incomplete', 'en-US', 'Content to complete')
ON CONFLICT (item_key, lang) DO NOTHING;
