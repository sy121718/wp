-- 149 · 访问统计后台菜单（BIZ-8）。
-- 与 084/090/093/097/101/107/110/113/140/143 同构：菜单（type=2）挂在「站点工程」目录下，绑定查看权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/analytics。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '访问统计',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/analytics', 'view.analytics', 'analytics:view', 1, 15, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '访问统计' AND type = 2 AND deleted_at IS NULL);
