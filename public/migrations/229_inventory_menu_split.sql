-- 229 · 库存独立成一级目录；仓库与变动原因字典各自成页
--
-- 背景（2026-09 后台设计评审第三轮）：
--   库存模块的页面原先全部挂在「商品与库存」目录下，与商品目录（商品 / 属性 / 分类 /
--   品牌 / 标签 / 定价 / 捆绑 / 详情模板）混在一起。直接的后果是「库存管理」一个页面要
--   同时容纳 库存流水 + 手动改库存 + 仓库配置 + 变动原因字典 四件事，只能靠 <details>
--   层层折叠 —— 折叠只是把「平铺」换成「叠起来」，装不下就该拆，而不是叠。
--   模块在代码里本来就是独立的（internal/module/product/inventory/），菜单应当反映这个边界。
--
-- 本迁移做四件事：
--   1) 一级目录「库存」插在「商品」之后（trade/site/system 的 sort 顺延一位）；
--   2) 库存管理 / 货源管理 / 采购入库 三项改挂到新目录；
--   3) 新增二级页「仓库管理」「变动原因字典」（权限码复用既有权限点）；
--   4) 重排库存目录内顺序：库存 → 仓库 → 货源 → 采购 → 原因字典。
--   「变更记录」(masterdata) 留在「商品与库存」不动 —— 它是商品与货源共用的主数据审计。
--
-- 幂等：注册见 register_catalog_inventory_data.go，CheckSQL 以
--       「/admin/inventory/warehouses 这条菜单是否存在」为门槛，整条迁移只跑一次。

-- 1) 先顺延（此刻「库存」尚未插入，不会把自己也加一）
UPDATE sys_menus SET sort_order = sort_order + 1, update_time = now()
WHERE coalesce(parent_id, 0) = 0 AND sort_order >= 4;

-- 2) 一级目录「库存」
INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
VALUES (NULL, '库存', 0, 1, '', 'warehouse', 1,
        0, 1, 1, 4, '库存模块一级目录：库存流水 / 仓库 / 货源 / 采购 / 变动原因', now(), now());

-- 3) 三项改挂到「库存」目录
UPDATE sys_menus
SET parent_id = (SELECT id FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND title = '库存' AND type = 1 ORDER BY id DESC LIMIT 1),
    update_time = now()
WHERE path IN ('/admin/inventory', '/admin/inventory/sources', '/admin/inventory/purchases');

-- 4) 目录内重排：库存=1、货源=3、采购=4（2 与 5 留给下面新增的两页）
UPDATE sys_menus SET sort_order = 1, update_time = now() WHERE path = '/admin/inventory';
UPDATE sys_menus SET sort_order = 3, update_time = now() WHERE path = '/admin/inventory/sources';
UPDATE sys_menus SET sort_order = 4, update_time = now() WHERE path = '/admin/inventory/purchases';

-- 5) 两个新页面（权限码指向既有权限点：页面可见性随该权限走）
INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
SELECT 'inventory:warehouse_list', '仓库管理', m.id, 2, '/admin/inventory/warehouses', '', 1,
       0, 1, 1, 2, '仓库实体：短码 / 默认仓 / 状态', now(), now()
FROM sys_menus m
WHERE coalesce(m.parent_id, 0) = 0 AND m.title = '库存' AND m.type = 1;

INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
SELECT 'inventory:reason_list', '变动原因字典', m.id, 2, '/admin/inventory/reasons', '', 1,
       0, 1, 1, 5, '库存变动原因的取值集合（内置 + 自定义）', now(), now()
FROM sys_menus m
WHERE coalesce(m.parent_id, 0) = 0 AND m.title = '库存' AND m.type = 1;
