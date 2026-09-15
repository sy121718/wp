-- 143 · 订单与优惠码后台菜单（BIZ-1）。
-- 与 084/090/093/097/101/107/110/113/140 同构：菜单（type=2）挂在「站点工程」目录下，绑定 list 权限点。
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/orders 与 /admin/coupons。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '订单管理',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/orders', 'view.order', 'order:list', 1, 13, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '订单管理' AND type = 2 AND deleted_at IS NULL);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '优惠码',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/coupons', 'view.order.coupon', 'order:coupon_list', 1, 14, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '优惠码' AND type = 2 AND deleted_at IS NULL);
