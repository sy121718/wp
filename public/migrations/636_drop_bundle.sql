-- ========================================
-- 636 · 捆绑品（bundle）概念整体下线：DB 侧收尾
--
-- 背景（2026-10 用户拍板）：「用新的仓库逻辑，不需要捆绑品这个概念 —— 所有品都是捆绑的，
-- 基数从商品下放到了仓库承担」。原模型里 type=bundle 的容器 + bundle_items 选项规则 +
-- inventory_bom_items 物料清单，是用「商品侧组合 + 扣减时展开」补出来的；库存基数改由
-- 仓库侧（inventory_stocks / 仓库 SKU）承担之后，这三块一起失去存在意义。
--
-- 代码侧已同批删除（service/dto/contract/handler/router/模板/runtimefragment/enums），
-- 本迁移只负责让存量库的形态跟上：
--   1. 物料清单表（102 建、104 给权限点）—— 表与两个权限点一起下线；
--   2. products.bundle_items 列与其形状约束（114）、GIN 索引（260）；
--   3. products.type 列与其枚举约束（238）、捆绑容器价约束（619）；
--   4. product:bundle_* 四个权限点与它们指向的六条 API 策略。
--
-- 为什么 DDL 写在 **Migration** 台账、而词条与菜单清理写在 **seed** 台账（636b）：
--   · DDL 是结构变更，必须在 Migrations 里（RunSeeds 恒在其后，见 assembly.go）；
--   · sys_i18n 词条与 sys_menus 行是数据，历史 seed（116/158/191/247/430/449 …）曾写过它们，
--     删除必须排在那些 seed 之后 —— 用 seed 台账才能保证「插入在前、删除在后」（418/419/458
--     的先例）。本批已把这些 seed 的 SQL 与幂等条件同批收口，所以两处都不再重建它们。
--
-- 为什么不改历史迁移文件（081 的建列、102 的建表、238/114/260/619 的约束）：
-- 迁移是变更历史，只追加不改写；新库由本迁移把结果改到终态，存量库由本迁移补齐。
-- 114 / 238 / 249 / 259 / 260 / 619 六个**纯捆绑**迁移已随本次改动整文件删除
--（它们的全部内容都是本迁移要抹掉的东西），故下表 DROP 语句全部带 IF EXISTS。
--
-- 幂等：DROP 全部带 IF EXISTS，DELETE 是条件删除，重复执行影响 0 行。
-- ========================================

-- ---- 1. 物料清单（issue #16）整表下线 ----

DROP TABLE IF EXISTS inventory_bom_items;

-- ---- 2. products 上的捆绑列 / 约束 / 索引 ----

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_bundle_items_shape_check;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_type_check;
ALTER TABLE products DROP CONSTRAINT IF EXISTS ck_products_bundle_price_positive;
DROP INDEX IF EXISTS idx_products_bundle_items_gin;
ALTER TABLE products DROP COLUMN IF EXISTS bundle_items;
ALTER TABLE products DROP COLUMN IF EXISTS type;

-- ---- 3. 权限点：捆绑配置 4 个 + 物料清单 2 个 ----
--
-- 先删策略再删权限点：sys_permission 被删后策略行会变成指向不存在权限码的死授权
--（与 122 的处理同形；104 的 seed 与本批同批收到 6 个，不会再插回 bom 两行）。
DELETE FROM sys_casbin_rule
 WHERE v1 IN ('/api/product/bundle/get', '/api/product/bundle/set',
              '/api/product/bundle/validate', '/api/product/bundle/skus',
              '/api/inventory/bom/set', '/api/inventory/bom/get');

DELETE FROM sys_permission
 WHERE permission_code IN ('product:bundle_get', 'product:bundle_set',
                           'product:bundle_validate', 'product:bundle_skus',
                           'inventory:bom_set', 'inventory:bom_get');
