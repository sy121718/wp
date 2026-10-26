-- ========================================
-- 634 — 后台菜单去重（库存目录被插了两套）+ 会员两项归位「客户」目录
--
-- 现场（开发库 2026-10-24 建库后实测，侧栏可直接肉眼看到）：
--   · 一级目录「库存」两条（id 2 / 192），库存子项被劈成两半：
--       /admin/inventory            -> 3(仓库管理)  + 58(库存管理)
--       /admin/inventory/warehouses -> 193(父 2)    + 194(父 192)
--       /admin/inventory/reasons    -> 4(父 2)      + 195(父 2) + 196(父 192)
--     侧栏因此出现两个「库存」，点哪个都只看到一半页面。
--   · 「会员等级与权益」/「会员归属」parent_id = 0，直接挂在侧栏根级。
--     463 按 title = '交易' 找父目录，而 224 建的分组直接叫「订单」（交易 → 订单的更名
--     在 555 才发生，且只对老库），COALESCE 落到 0。555 又把客户域独立成「客户」目录：
--     会员等级的门槛按订单消费额算，它本来就属于客户目录。
--
-- 为什么库存会有两套（根因，本批一并修）：
--   229 的 CheckSQL 以「/admin/inventory/warehouses 这一行存在」为门槛，而 224 第 2 段
--   按 permission_code 改 path —— inventory:warehouse_list 同时被「库存管理」与
--   「仓库管理」两页使用，224 重跑时把 229 刚插入的仓库页 path 改回 /admin/inventory，
--   于是 229 的判据失配、下一次启动整条重跑，又插一套目录 + 子页。
--   本迁移只负责把**已经写坏的结果**收敛；229 的判据与守卫、224 的匹配条件在同批改掉。
--
-- 幂等：所有 UPDATE 都以「结果是否已达成」为条件（值相同则改动 0 行），重复执行安全。
-- ========================================

-- ---- 1) 库存子项先统一挂到最后插入的那条目录，再软删多余目录 ----
--
-- 保留 id 最大的那条：它是 229 最近一次跑出来的完整形态（sort_order = 4，紧跟
-- 「商品与库存」），而先插的那条 sort 被后续顺延过、与订单目录撞号。
-- 子项先迁后删，任何时刻都不会出现「父目录已删、子项还挂在它下面」的孤儿窗口。

WITH dirs AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY id DESC) AS rn
    FROM sys_menus
    WHERE deleted_at IS NULL AND type = 1 AND COALESCE(parent_id, 0) = 0 AND title = '库存'
)
UPDATE sys_menus m
SET parent_id = (SELECT id FROM dirs WHERE rn = 1), update_time = NOW()
FROM dirs d
WHERE m.deleted_at IS NULL AND m.type = 2 AND m.parent_id = d.id AND d.rn > 1;

UPDATE sys_menus
SET deleted_at = NOW(), update_time = NOW()
WHERE deleted_at IS NULL AND type = 1 AND COALESCE(parent_id, 0) = 0 AND title = '库存'
  AND id <> (
      SELECT MAX(k.id) FROM sys_menus k
      WHERE k.deleted_at IS NULL AND k.type = 1 AND COALESCE(k.parent_id, 0) = 0 AND k.title = '库存'
  );

-- ---- 2) 同一 path 只留一行 ----
--
-- 保留规则：先看 title 是否已经是该页的正确名字（/admin/inventory 被 224 改成了
-- 「仓库管理」，要留的是「库存管理」那条），再按 id 取最小 —— 老行被角色按 menu_id
-- 授权过，删除它的同路径替身而不是它，是这里唯一有意义的偏向。
--
-- 软删而非物理删：角色授权按 menu_id 收集 permission_code（GetPermissionCodesByIDs），
-- 删行会让已授权角色静默缩权。软删的行不进授权树，但行还在。

WITH keep AS (
    SELECT DISTINCT ON (path) id
    FROM sys_menus
    WHERE deleted_at IS NULL AND type = 2
      AND path IN ('/admin/inventory', '/admin/inventory/warehouses', '/admin/inventory/reasons')
    ORDER BY path,
             (title = CASE path
                          WHEN '/admin/inventory' THEN '库存管理'
                          WHEN '/admin/inventory/warehouses' THEN '仓库管理'
                          ELSE '变动原因字典'
                      END) DESC,
             id
)
UPDATE sys_menus m
SET deleted_at = NOW(), update_time = NOW()
WHERE m.deleted_at IS NULL AND m.type = 2
  AND m.path IN ('/admin/inventory', '/admin/inventory/warehouses', '/admin/inventory/reasons')
  AND m.id NOT IN (SELECT id FROM keep);

-- ---- 3a) 三个老子页归位到「库存」目录 ----
--
-- 229 是结构迁移，224 是 seed：新库上 229 跑在 224 之前，那时 /admin/inventory、
-- sources、purchases 三行还不存在，229 第 3 段的改挂影响 0 行 —— 于是库存子项
-- 一直挂在「商品与库存」(96) 下，而仓库页与原因字典页挂在「库存」下，目录被劈成两半。
-- 老库上之所以看起来对，是因为 229 后来因判据失配重跑过一次（就是重复目录的来源）。

UPDATE sys_menus m
SET parent_id = d.id, update_time = NOW()
FROM (
    SELECT id FROM sys_menus
    WHERE deleted_at IS NULL AND type = 1 AND COALESCE(parent_id, 0) = 0 AND title = '库存'
    ORDER BY id DESC
    LIMIT 1
) d
WHERE m.deleted_at IS NULL AND m.type = 2
  AND m.path IN ('/admin/inventory', '/admin/inventory/sources', '/admin/inventory/purchases')
  AND m.parent_id <> d.id;

-- ---- 3) 库存目录内的标题与顺序（229 的设计顺序，写死在这里做收口） ----

UPDATE sys_menus m
SET title = v.title, sort_order = v.sort, update_time = NOW()
FROM (VALUES
    ('/admin/inventory',            '库存管理',     1),
    ('/admin/inventory/warehouses', '仓库管理',     2),
    ('/admin/inventory/sources',    '货源管理',     3),
    ('/admin/inventory/purchases',  '采购入库',     4),
    ('/admin/inventory/reasons',    '变动原因字典', 5)
) AS v(path, title, sort)
WHERE m.deleted_at IS NULL AND m.type = 2 AND m.path = v.path
  AND (m.title <> v.title OR m.sort_order <> v.sort);

-- ---- 4) 会员两项归位「客户」目录 ----
--
-- 客户目录内现有顺序：客户概览 1 / 客户列表 2 / RFM 分析 3 / 群组留存 4，
-- 会员两项接在 5 与 6。目录不存在时 CROSS JOIN 为空 → 不改动（保留原 parent），
-- 比把 parent_id 写成 0 再把菜单扔到根级要好。

UPDATE sys_menus m
SET parent_id = c.id, sort_order = v.sort, update_time = NOW()
FROM (VALUES ('/admin/membership', 5), ('/admin/membership/assignments', 6)) AS v(path, sort)
CROSS JOIN LATERAL (
    SELECT id FROM sys_menus
    WHERE deleted_at IS NULL AND type = 1 AND COALESCE(parent_id, 0) = 0 AND title = '客户'
    ORDER BY id
    LIMIT 1
) c
WHERE m.deleted_at IS NULL AND m.type = 2 AND m.path = v.path
  AND (m.parent_id <> c.id OR m.sort_order <> v.sort);

-- ---- 5) 一级目录不撞号 ----
--
-- 229 的「sort_order + 1」跑过两遍，站点 7 / 系统 8 / 客户 8 —— 客户与系统同号，
-- 顺序只能靠 id 兜底。555 的本意是客户排在最后（它写的是 7，当时系统是 6）。

UPDATE sys_menus
SET sort_order = 9, update_time = NOW()
WHERE deleted_at IS NULL AND type = 1 AND COALESCE(parent_id, 0) = 0 AND title = '客户'
  AND sort_order <> 9;
