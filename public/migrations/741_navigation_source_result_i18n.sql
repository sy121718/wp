-- 导航「从来源加入菜单」改为独立端点 + Jet HTML 结果片段：补齐计数词条中英两行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('navSrcNone', 'zh-CN', '没有选中任何条目，菜单未改动。', 400, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcNone', 'en-US', 'No entries selected; the menu is unchanged.', 400, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcSkip', 'zh-CN', '没有条目被加入菜单：{skipped} 个被跳过。', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcSkip', 'en-US', 'No entry was added: {skipped} were skipped.', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcDone', 'zh-CN', '已把 {count} 个条目加入菜单。', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcDone', 'en-US', 'Added {count} entries to the menu.', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcPart', 'zh-CN', '已加入 {added} 个条目，另有 {skipped} 个被跳过。', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now()),
('navSrcPart', 'en-US', 'Added {added} entries; {skipped} were skipped.', 200, 'navigation', 'admin/navigation/navigation_add_source_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
