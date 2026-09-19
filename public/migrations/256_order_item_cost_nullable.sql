-- 256 · 订单行成本快照改为「归属仓当前成本」的两处结构变更（2026-09-19 商品域评审收口项）。
--
-- 口径（docs/14-product-sku-and-cost-model.md §1.3 / §4.2 / §4.3 / §9.3，用户已定，不自行改口径）：
--   · 成本真源是 inventory_stocks.cost_price（(仓库, SKU) 的当前成本，迁移 244）；
--   · 订单行的成本快照取自「该变体在**该行归属仓**的当前成本」；归属仓 = 行上显式 WarehouseID，
--     为空则按库存域既有的归属仓解析规则（默认仓）解析；
--   · **未核算（cost_price IS NULL）时订单行成本留空** —— 绝不用 0 冒充；
--     0 是合法的显式成本（赠品 / 内部划拨），「未知」与「零成本」是两回事。
--
-- 两件事：
--   1. order_items.cost_price：135 建表时是 BIGINT NOT NULL DEFAULT 0（默认值会把「没传成本」
--      悄悄记成 0，正是要消灭的冒充），这里改成**可空且无默认值**；NULL = 下单时该
--      (仓库, SKU) 尚未核算。
--   2. inventory_stock_movements.unit_cost：出库 / 入库时刻的单位成本留痕 ——
--      出库方向的流水在写入时从该库存行复制当时的 cost_price，「实际发出那批货的成本」
--      不再被后续改价影响；入库方向记本次显式成本（采购单价 / 生产单价），无显式成本时
--      记库存行当前成本（口径与理由见 inventory/service 的 movementOf）。
--
-- 为什么 CheckSQL 要判「列已可空 + 已无默认值 / 列已存在」而不是只判存在
--（DB-015 的坑，240/244/251 同手法）：只判「cost_price 这一列在不在」会永远为真
--（135 早就建过它），迁移被静默跳过 —— 于是「列存在但仍是 NOT NULL DEFAULT 0」的状态
-- 永远修不好，而启动日志一切正常。unit_cost 同理：只判表在也会误跳过。
--
-- 幂等：ALTER COLUMN ... DROP NOT NULL / DROP DEFAULT 重复执行无害；
-- ADD COLUMN IF NOT EXISTS 自带幂等。movements 是分区表，父表 ADD COLUMN 由 PG
-- 自动传播到全部分区（PG 11+，CI 是 PG 16）。

-- 1. 订单行成本可空、去掉 0 默认值。
ALTER TABLE order_items ALTER COLUMN cost_price DROP NOT NULL;
ALTER TABLE order_items ALTER COLUMN cost_price DROP DEFAULT;

COMMENT ON COLUMN order_items.cost_price IS '下单时刻的行成本快照（分，来自该行归属仓的当前成本）；NULL = 下单时该 (仓库, SKU) 尚未核算（绝不用 0 冒充）';

-- 2. 流水侧的成本留痕（出库复制当时库存行成本；入库记本次显式成本）。
ALTER TABLE inventory_stock_movements ADD COLUMN IF NOT EXISTS unit_cost numeric(12,2);

COMMENT ON COLUMN inventory_stock_movements.unit_cost IS '这次变动时刻的单位成本留痕（元）：出库 = 扣减时该库存行的当前成本；入库 = 本次显式成本（采购 / 生产单价），无显式成本时记库存行当前成本；NULL = 当时尚未核算';
