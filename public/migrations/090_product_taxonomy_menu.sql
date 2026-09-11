-- 090 · 商品分类与品牌后台菜单（issue #10）。
-- 与 084/086b 同构：菜单（type=2）挂在「商品管理」目录下，绑定各自的 list 权限点；
-- 按钮（type=3）挂在对应菜单下，绑定各自的写权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/product-categories 与 /admin/product-brands。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '商品分类',
       -- 找不到父菜单时落到顶级（0）：测试 schema 与精简部署都可能没有「商品管理」。
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品管理' AND type = 2 AND deleted_time IS NULL), 0),
       2, '/product-categories', 'view.product.categories', 'product:category_list', 1, 9, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '商品分类' AND type = 2 AND deleted_time IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '商品品牌',
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品管理' AND type = 2 AND deleted_time IS NULL), 0),
       2, '/product-brands', 'view.product.brands', 'product:brand_list', 1, 10, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '商品品牌' AND type = 2 AND deleted_time IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = v.parent AND type = 2 AND deleted_time IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('商品分类', '新建商品分类', 3, 'product:category_create', 20),
    ('商品分类', '更新商品分类', 3, 'product:category_update', 21),
    ('商品分类', '删除商品分类', 3, 'product:category_delete', 22),
    ('商品品牌', '新建商品品牌', 3, 'product:brand_create',    30),
    ('商品品牌', '更新商品品牌', 3, 'product:brand_update',    31),
    ('商品品牌', '删除商品品牌', 3, 'product:brand_delete',    32)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_time IS NULL);
