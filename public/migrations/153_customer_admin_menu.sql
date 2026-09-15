-- 153 · 客户管理后台菜单（sys_menus）。
-- 与 140/143/149/150 同构：菜单（type=2）挂在「站点工程」目录下，绑定查看权限点
-- （user:customer_list，来自迁移 152）。未 seed 时页面本身仍可直接访问 /admin/customers。
--
-- 注意两份菜单配置的分工（与 140/150 同口径，别把这里当成侧栏真源）：
--   · 后台侧栏由代码配置 internal/module/dashboard/inbound/http/nav_menu.go 的 navConfig 驱动
--     （「系统」分组下的「客户管理」节点）；
--   · 本 seed 写的是 sys_menus，供后台「菜单管理」页展示与权限菜单分配使用。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '客户管理',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/customers', 'view.customer', 'user:customer_list', 1, 16, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '客户管理' AND type = 2 AND deleted_at IS NULL);
