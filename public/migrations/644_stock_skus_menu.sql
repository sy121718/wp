-- 644 · 仓库商品 SKU 台账页菜单 + 两个按钮（迁移 642 / B3）。
--
-- 背景：/admin/inventory/skus 这个路径在「去认领」（迁移 638）时随旧台账页一起下线。
-- 本次按仓库主档重建同一屏 —— 货行现在有自己的主档（名称 / 图 / 条码 / 成本四件套 /
-- 运费 / 状态 / 备注），值得一屏把自己的行列出来，而不是只能从商品页反查。
--
-- 与「库存管理」（/admin/inventory）的分工：
--   · 库存管理 = 按 SKU 横看各仓（入口是筛一个 SKU）；
--   · 本页 = 按仓纵看一屏货行（入口是筛一个仓）。
-- 两屏读同一张表（inventory_stocks），维度不同，所以是两个菜单项而不是一个页签。
--
-- 幂等：两条 INSERT 都用 path / permission_code 的 NOT EXISTS 守卫；
-- 父目录按 path 找「库存管理」目录（192 那一条），找不到时落顶级（0）——
-- 精简部署与测试 schema 都可能没有那个目录。

INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '仓库商品 SKU',
       'admin.inventory.stock_skus.title',
       COALESCE((SELECT id FROM sys_menus WHERE path = '/admin/inventory' AND type = 2 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), 0),
       2, '/admin/inventory/skus', 'view.inventory.stock_skus', 'inventory:stock_list', 1, 6, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/inventory/skus' AND deleted_at IS NULL);

-- 两个按钮（type=3）挂在台账菜单下，title_key 就是模板里 isset(.Buttons[...]) 的键：
--   · inventory.stock_create → 新建一条货行（复用 inventory:stock_ensure）；
--   · inventory.stock_update → 改主档 / 启停（复用 inventory:stock_change）。
-- 复用既有权限点而不新造：sys_permission 已有那两行，另起名字等于给同一能力发两张票。
INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title, v.title_key,
       COALESCE((SELECT id FROM sys_menus WHERE path = '/admin/inventory/skus' AND type = 2 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('新建仓库 SKU', 'inventory.stock_create', 3, 'inventory:stock_ensure', 70),
    ('修改仓库 SKU', 'inventory.stock_update', 3, 'inventory:stock_change', 71)
) AS v(title, title_key, type, code, sort)
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus x
    WHERE x.title_key = v.title_key AND x.type = 3 AND x.deleted_at IS NULL
);
