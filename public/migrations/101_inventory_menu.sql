-- 101 · 库存管理后台菜单（issue #15）。
-- 与 084/086b/090/093/097 同构：菜单（type=2）挂在「站点工程」目录下，绑定 list 权限点；
-- 按钮（type=3）挂在菜单下，绑定各自的写权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/inventory。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '库存管理',
       -- 找不到父目录时落到顶级（0）：测试 schema 与精简部署都可能没有「站点工程」目录。
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL), 0),
       2, '/inventory', 'view.inventory', 'inventory:warehouse_list', 1, 8, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '库存管理' AND type = 2 AND deleted_time IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = v.parent AND type = 2 AND deleted_time IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('库存管理', '新建仓库', 3, 'inventory:warehouse_create', 60),
    ('库存管理', '修改仓库', 3, 'inventory:warehouse_update', 61),
    ('库存管理', '删除仓库', 3, 'inventory:warehouse_delete', 62)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_time IS NULL);
