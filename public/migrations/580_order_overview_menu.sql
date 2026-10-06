-- 580 · 订单目录的「销售概览」菜单（BIZ-1 销售侧）。
--
-- 与 143/149/561 同构：type=2 的菜单节点挂在「订单」目录（type=1）下，
-- 绑定的权限点是 order:overview —— 该权限点由代码声明（internal/permission/codes.go
-- 的 OrderOverview + order_router.go 里的 permission.Declare），启动期 SyncToDB
-- 幂等 upsert 进 sys_permission，**本迁移不插 sys_permission 行**（AGENTS.md §数据库：
-- 新增权限点加常量 + 在路由注册处声明，不写权限点 seed 迁移）。
--
-- 写 permission_code（旧列）是正确的：迁移 470 的触发器会把这一列单向补进
-- sys_menu_permission（多对多正源），读路径只读新表。这里不必也不该手写关联表。
--
-- 幂等判据按 **path** 而不是 title：「/admin/orders/overview」是这一行的身份，
-- 而 title 是运营可以在后台改的中文名（改完再启动就会重复插一行菜单）。
-- 判据刻意只数本批要插的那一行（上界封闭），不用「订单目录下的菜单总数」这类
-- 会随别的批次漂移的量 —— 那正是 058/076 两次真实故障的形状。
--
-- 未 seed 时后台侧栏看不到入口，页面本身仍可直接访问 /admin/orders/overview
-- （整页只读，不挂 Casbin 中间件），订单列表页页头也有入口按钮。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '销售概览',
       COALESCE((SELECT id FROM sys_menus WHERE title = '订单' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/admin/orders/overview', 'view.order.sales', 'order:overview', 1, 3, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus WHERE type = 2 AND path = '/admin/orders/overview' AND deleted_at IS NULL
);
