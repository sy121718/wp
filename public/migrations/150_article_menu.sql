-- 150 · 文章管理菜单（INF-1）。
-- 与 084/090/093/097/101/107/110/113/140/143/149 同构：菜单（type=2）挂在「内容」目录下，绑定查看权限点。
--
-- 注意两份菜单配置的分工（与 140 同口径，别把这里当成侧栏真源）：
--   · 后台侧栏由代码配置 internal/module/dashboard/inbound/http/nav_menu.go 的 navConfig 驱动
--     （「内容」分组下的「文章」节点）；
--   · 本 seed 写的是 sys_menus，供后台「菜单管理」页展示与权限菜单分配使用。
-- 两者数据源不同、用途不同，不存在漂移 —— 只是都要有，否则菜单管理页里看不到这个入口。
--
-- 未 seed 时页面本身仍可直接访问 /admin/articles（路由不依赖菜单）。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '文章',
       COALESCE((SELECT id FROM sys_menus WHERE title = '内容' AND type = 1 AND deleted_time IS NULL), 0),
       2, '/articles', 'view.articles', 'content:list', 1, 5, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '文章' AND type = 2 AND deleted_time IS NULL);
