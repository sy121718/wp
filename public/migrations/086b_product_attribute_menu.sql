-- 086b · 商品属性后台菜单（issue #7）。
-- 与 084 同构：菜单（type=2）挂在「商品管理」目录下，绑定 product:attribute_list 权限点；
-- 按钮（type=3）挂在「商品属性」菜单下，绑定各自的写权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/product-attributes。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '商品属性',
       -- 找不到父菜单时落到顶级（0）：测试 schema 与精简部署都可能没有「商品管理」。
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品管理' AND type = 2 AND deleted_time IS NULL), 0),
       2, '/product-attributes', 'view.product.attributes', 'product:attribute_list', 1, 8, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '商品属性' AND type = 2 AND deleted_time IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品属性' AND type = 2 AND deleted_time IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('新建属性组', 3, 'product:attribute_create',     10),
    ('更新属性组', 3, 'product:attribute_update',     11),
    ('删除属性组', 3, 'product:attribute_delete',     12),
    ('保存属性值', 3, 'product:attribute_set_values', 13)
) AS v(title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_time IS NULL);
