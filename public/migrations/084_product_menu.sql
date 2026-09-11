-- 084_product_menu.sql — 商品管理后台菜单（issue #5 / T3a）。
-- 与 030 同构：菜单（type=2）挂在「站点工程」目录下，绑定 product:list 权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/products。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '商品管理',
       -- 找不到父目录时落到顶级（0）：测试 schema 与精简部署都可能没有「站点工程」目录。
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL), 0),
       2, '/products', 'view.product', 'product:list', 1, 7, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '商品管理' AND type = 2 AND deleted_time IS NULL);