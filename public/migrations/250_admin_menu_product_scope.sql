-- 250 · 商品目录收口：定价工具 / 捆绑配置 下线，商品详情模板改挂「内容」目录
--
-- 背景（2026-09 后台设计评审第四轮，用户逐条提出的归属问题）：
--   · 定价工具独立成菜单是错的 —— 它作用的对象是「一批筛选出来的商品」（列表页的批量操作），
--     或单个商品的改价（商品详情）。批量入口已落到商品列表的批量操作区，独立页面退居备份。
--   · 捆绑不是一种「配置页」，而是商品类型的一种：商品详情页在 type=bundle 时显示「捆绑构成」，
--     配置器页面保留（详情页入口指向它），但不应再占一个菜单位。
--   · 商品详情模板不是商品域的东西 —— 它描述「商品详情页长什么样」，属于页面/区块那一层。
--     本迁移先把**菜单归属**归位（移到「内容」目录）；能力实现的搬迁另批进行。
--
-- 注册为 **seed**（registerSeed）而不是 Migration：这三行菜单由 097 / 116 / 224 三条
-- seed 建出来，而 migrations.Run 先于 RunSeeds —— 按 Migration 注册会在干净库上
-- 判定「没有菜单可改」而静默跳过，新装环境永远拿不到归位。
-- 幂等：三处 UPDATE 都是「已是目标状态则不动」，可重复执行。
-- 下线用 status = 0（导航树只认 status = 1，见 menu_model.go 的注释），
-- 不用 deleted_at：菜单管理页仍能看到并恢复它们。

UPDATE sys_menus SET status = 0, update_time = now()
 WHERE deleted_at IS NULL AND status <> 0
   AND path IN ('/admin/product-pricing', '/admin/products/bundle');

-- 商品详情模板改挂「内容」目录（与页面管理 / 区块管理同层），排在最后。
-- 父级按 title 定位（与 229 的写法一致）：按 sort_order 取第一条会命中「仪表盘」——
-- 那是 type=1 里 sort 最小的一个，不是「内容」。
UPDATE sys_menus
   SET parent_id = (SELECT id FROM sys_menus
                     WHERE coalesce(parent_id, 0) = 0 AND type = 1 AND title = '内容' AND deleted_at IS NULL
                     ORDER BY id ASC LIMIT 1),
       sort_order = 9,
       update_time = now()
 WHERE deleted_at IS NULL
   AND path = '/admin/products/template'
   AND parent_id <> (SELECT id FROM sys_menus
                      WHERE coalesce(parent_id, 0) = 0 AND type = 1 AND title = '内容' AND deleted_at IS NULL
                      ORDER BY id ASC LIMIT 1);
