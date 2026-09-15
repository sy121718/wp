-- 110 · 采购入库后台菜单（issue #18）。
-- 与 084/090/093/097/101/107 同构：菜单（type=2）挂在「站点工程」目录下，绑定 list 权限点；
-- 按钮（type=3）挂在菜单下，绑定各自的写权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/inventory/purchases。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '采购入库',
       -- 找不到父目录时落到顶级（0）：测试 schema 与精简部署都可能没有「站点工程」目录。
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/inventory/purchases', 'view.inventory.purchases', 'inventory:purchase_list', 1, 10, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '采购入库' AND type = 2 AND deleted_at IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = '采购入库' AND type = 2 AND deleted_at IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('采购入库', '新建采购单', 3, 'inventory:purchase_create', 80),
    ('采购入库', '采购收货入库', 3, 'inventory:purchase_receipt', 81),
    ('采购入库', '生产入库', 3, 'inventory:purchase_production', 82)
) AS v(title, parent, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_at IS NULL);
