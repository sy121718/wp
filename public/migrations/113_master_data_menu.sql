-- 113 · 主数据变更记录后台菜单（issue #19）。
-- 与 084/090/093/097/101/107/110 同构：菜单（type=2）挂在「站点工程」目录下，绑定 list 权限点。
-- 只读页面，故不建按钮（type=3）。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/masterdata/changes。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '变更记录',
       -- 找不到父目录时落到顶级（0）：测试 schema 与精简部署都可能没有「站点工程」目录。
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL), 0),
       2, '/masterdata/changes', 'view.masterdata.changes', 'masterdata:change_list', 1, 11, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '变更记录' AND type = 2 AND deleted_time IS NULL);
