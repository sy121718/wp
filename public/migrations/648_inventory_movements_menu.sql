-- 648 · 库存流水独立页的菜单项（B4）。
--
-- 背景：流水原先只是 /admin/inventory 页面里的第三块。它的读取维度是**跨 SKU 的时间轴**
-- （谁在什么时候动了哪条货），而那一屏的入口是「筛一个 SKU 看它的库存」——
-- 挤在一起时想看全局流水就得清空 SKU，而清空又同时清掉了库存卡的上下文。
--
-- 挂载点与「仓库商品 SKU」（迁移 644）同级：都挂在「库存管理」这一条（path='/admin/inventory'）
-- 下面，因为它们是同一张 inventory_stocks 的两种看法（按 SKU 横看 / 按时间轴纵看）。
--
-- 幂等：NOT EXISTS(path) 守卫；父级按 path 找，找不到时落顶级（0）——
-- 精简部署与测试 schema 都可能没有那一条。
INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '库存流水',
       'admin.inventory.movements.title',
       COALESCE((SELECT id FROM sys_menus WHERE path = '/admin/inventory' AND type = 2 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), 0),
       2, '/admin/inventory/movements', 'view.inventory.movements', 'inventory:movement_list', 1, 7, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/inventory/movements' AND deleted_at IS NULL);
