-- 097 · 定价工具后台菜单（issue #13）。
-- 与 084/086b/090/093 同构：菜单（type=2）挂在「商品管理」目录下，绑定 rules 权限点；
-- 按钮（type=3）挂在菜单下，绑定各自的写权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/product-pricing。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '定价工具',
       -- 找不到父菜单时落到顶级（0）：测试 schema 与精简部署都可能没有「商品管理」。
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品管理' AND type = 2 AND deleted_time IS NULL), 0),
       2, '/product-pricing', 'view.product.pricing', 'product:pricing_rules', 1, 12, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '定价工具' AND type = 2 AND deleted_time IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = v.parent AND type = 2 AND deleted_time IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('定价工具', '预览定价', 3, 'product:pricing_preview', 50),
    ('定价工具', '应用定价', 3, 'product:pricing_apply', 51)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_time IS NULL);
