-- 121_drop_stock_cache.sql
-- 去掉商品侧库存缓存（issue #32：商品与库存合并为同一模块后不再需要缓存副本）。
--
-- 背景：#16 引入 product_variants.stock_total / stock_synced_at 是因为两个模块的表不能互相 JOIN，
-- 只好在商品侧存一份真源汇总；为此又必须处理「提交之后同步」带来的一致性窗口 ——
-- 于是有台账与对账。合并模块之后这些补偿机制失去存在理由：
-- 商品查询直接读 inventory_stocks 真源做**查询期投影**，没有窗口、没有台账、没有对账。
--
-- 删除内容：
--   · product_variants.stock_total / stock_synced_at 两个展示缓存列；
--   · inventory_stock_cache_syncs 台账表（它记录的是「缓存同步结果」，缓存没了它也没了）。
--
-- 死线不变：可用量判断仍然只读 inventory_stocks 真源（带行锁）。
ALTER TABLE product_variants DROP COLUMN IF EXISTS stock_total;
ALTER TABLE product_variants DROP COLUMN IF EXISTS stock_synced_at;
DROP TABLE IF EXISTS inventory_stock_cache_syncs;

COMMENT ON TABLE inventory_stocks IS '库存真源（SKU × 仓库）；商品侧不再有缓存副本，展示值按需投影（issue #32）'
