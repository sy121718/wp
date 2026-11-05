-- 导航批量删除改为 Jet HTML 结果片段：补齐计数与空选提示的中英词条。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('navBulkNone', 'zh-CN', '没有选中任何导航项，列表未改动。', 400, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkNone', 'en-US', 'No navigation items selected; the list is unchanged.', 400, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkSkip', 'zh-CN', '没有导航项被删除：{skipped} 个失败。', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkSkip', 'en-US', 'No navigation item was deleted: {skipped} failed.', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkDone', 'zh-CN', '已删除 {count} 个导航项。', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkDone', 'en-US', 'Deleted {count} navigation items.', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkPart', 'zh-CN', '已删除 {done} 个导航项，另有 {skipped} 个未删除。', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now()),
('navBulkPart', 'en-US', 'Deleted {done} navigation items; {skipped} were not deleted.', 200, 'navigation', 'admin/navigation/navigation_bulk_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
