-- 662 · 商品发布记录页（B9）的菜单项。
--
-- 挂载点：「商品与库存」目录（与 /admin/products 同级）。它是商品的**发布视角**，
-- 与商品列表看的是同一批实体（列表 = 有哪些商品，本页 = 这些商品发到哪了），
-- 所以放在同一个目录里而不是「站点」目录：运营从商品出发找发布状态，不会先去站点。
--
-- 权限码复用 product:list：本页只读商品与它们的发布登记，读的就是商品列表能看的东西
-- （页面路由用 adminui.PageAuthz("/api/product/list") 判）。另造一个只为本页存在的
-- 权限码等于给同一批数据发两张票。
--
-- 幂等：NOT EXISTS(path) 守卫；父目录按 title 找（与 250 的写法一致 —— 按 sort_order
-- 取第一条会命中别的目录），找不到时落顶级（0）：精简部署与测试 schema 都可能没有那条目录。
INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '发布记录',
       'admin.products.publish.title',
       COALESCE((SELECT id FROM sys_menus WHERE title = '商品与库存' AND type = 1 AND deleted_at IS NULL ORDER BY id ASC LIMIT 1), 0),
       2, '/admin/products/publish-records', 'view.products.publish_records', 'product:list', 1, 7, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/products/publish-records' AND deleted_at IS NULL);
