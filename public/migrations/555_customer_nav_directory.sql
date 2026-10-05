-- 555 · 客户目录：新建一级分组「客户」，「交易」更名「订单」
--
-- 背景（docs/17 §P7）：客户域从「交易」组里独立出来，成为一级目录；「交易」这个名字
-- 此时已经名不副实 —— 组里放的是订单、退货、优惠码、客户，四样东西只有前两样算「交易」。
--
-- 做四件事：
--   1. 分组「交易」改名「订单」（老库才有这个旧名；新库在 224 里直接建的就是「订单」，
--      这条对它是空操作 —— SQL 幂等，两种起点收敛到同一个结果）；
--   2. 建一级分组「客户」；
--   3. 原「客户管理」菜单移进「客户」组，标题改成「客户列表」（与新目录的分工一致：
--      分组叫「客户」，里面的列表项叫「客户列表」）；
--   4. 补「客户概览」菜单。
--
-- **为什么必须同时改 224 的三处 '交易'**：224 是 seed，它的 ConditionSQL 按
-- `title IN ('管理','内容','商品与库存','交易','站点','系统')` 判定「收口已完成」。
-- 只改本迁移、不改 224，那条判据就永远不满足 → 224 每次启动重跑 → 而它的 INSERT 会把
-- 已改名的「订单」当成缺失，重新插一个空的「交易」分组出来。**SQL 幂等挡不住
-- 「判据与它判定的对象脱节」** —— 本仓在 076 / 058 上各栽过一次同类。
--
-- 权限点复用 user:customer_list（概览与列表是同一份数据的两种看法），不新增 ——
-- 多一个权限点就多一处需要维护的授权真相，而「看得到列表」与「看得到概览」在业务上
-- 不是两件事。

-- ---- 1. 分组更名 ----
UPDATE sys_menus
SET title = '订单', update_time = NOW()
WHERE type = 1 AND title = '交易' AND deleted_at IS NULL;

-- ---- 2. 一级分组「客户」----
-- sort_order 取 7：排在「系统」(6) 之后 —— 客户是新目录，插进中间会让所有既有
-- 分组的序号跟着挪一遍（一次改动波及六行，而收益只是「位置好看一点」）。
INSERT INTO sys_menus (title, parent_id, type, path, icon, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '客户', 0, 1, '', 'users', 1, 7, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '客户' AND type = 1 AND deleted_at IS NULL);

-- ---- 3. 「客户管理」移入「客户」组并改名 ----
-- parent_id 保留原值兜底：父分组查不到时宁可不移动，也不要把它挂到根上（parent_id = 0
-- 会让它在一级目录里凭空出现一个同名项）。
--
-- WHERE 要覆盖**两种历史 path**：老库上这一行是 153 seed 插的，写的是 `/customers`；
-- 224 之后的库里是 `/admin/customers`。只匹配后者会让老库那一行永远留在原地
--（侧栏里出现两个客户入口），而迁移本身看不出任何异常。
UPDATE sys_menus m
SET parent_id = COALESCE((
        SELECT p.id FROM sys_menus p
         WHERE p.title = '客户' AND p.type = 1 AND p.deleted_at IS NULL
    ), m.parent_id),
    title = '客户列表',
    path = '/admin/customers',
    sort_order = 2,
    update_time = NOW()
WHERE m.type = 2 AND m.path IN ('/customers', '/admin/customers') AND m.deleted_at IS NULL;

-- ---- 3b. 仍然缺失就插入 ----
-- **新库走这条**：153 seed 已注销（理由见 register_analytics.go 里那段注释），
-- 而 224 的两处 `/admin/customers` 只是**映射表的键**，只 UPDATE 已存在的行、不新建。
-- 于是「只跑迁移 + 种子」的新库里压根没有这一行 —— 上面的 UPDATE 影响 0 行、不报错，
-- 菜单树看起来正常（少了客户入口），只有按 path 断言的测试会红。
-- 这类「上游假设某行已存在、而提供它的那条 seed 已被注销」的缺口，
-- 判据必须落在**结果**上（这一行在不在），不能落在过程上（我改没改名）。
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '客户列表',
       COALESCE((
           SELECT p.id FROM sys_menus p
            WHERE p.title = '客户' AND p.type = 1 AND p.deleted_at IS NULL
       ), 0),
       2, '/admin/customers', 'users', 'user:customer_list', 1, 2, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus WHERE type = 2 AND path = '/admin/customers' AND deleted_at IS NULL
);

-- ---- 4. 新增「客户概览」----
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '客户概览',
       COALESCE((
           SELECT p.id FROM sys_menus p
            WHERE p.title = '客户' AND p.type = 1 AND p.deleted_at IS NULL
       ), 0),
       2, '/admin/customers/overview', '', 'user:customer_list', 1, 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus WHERE type = 2 AND path = '/admin/customers/overview' AND deleted_at IS NULL
);
