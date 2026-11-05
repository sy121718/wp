-- 导航写操作改为 Jet HTML 结果片段：补齐译文保存结论的短词条。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('navTrSaved', 'zh-CN', '已保存 {count} 条译文。', 200, 'navigation', 'admin/navigation/navigation_translations_saved.html', 1, now(), now()),
('navTrSaved', 'en-US', 'Saved {count} translations.', 200, 'navigation', 'admin/navigation/navigation_translations_saved.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
