-- 707: 本地规则编辑器换域反馈，默认值不覆盖后台编辑。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
 ('admin.datarules.editor.domain_conflict', 'zh-CN', '新数据域与当前条件或屏蔽字段不匹配，请先调整配置', 200, 'ui', '规则编辑器', 1, now(), now()),
 ('admin.datarules.editor.domain_conflict', 'en-US', 'The new domain does not support the current conditions or omitted fields. Adjust the configuration first.', 200, 'ui', '规则编辑器', 1, now(), now()),
 ('admin.datarules.editor.load_failed', 'zh-CN', '字段加载失败，请重试', 200, 'ui', '规则编辑器', 1, now(), now()),
 ('admin.datarules.editor.load_failed', 'en-US', 'Fields could not be loaded. Please try again.', 200, 'ui', '规则编辑器', 1, now(), now()),
 ('admin.datarules.editor.loading', 'zh-CN', '正在加载字段，请稍候', 200, 'ui', '规则编辑器', 1, now(), now()),
 ('admin.datarules.editor.loading', 'en-US', 'Loading fields. Please wait.', 200, 'ui', '规则编辑器', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
