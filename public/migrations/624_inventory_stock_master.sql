-- 621 · 仓库商品 SKU 主档化：允许「仓库里已有、尚未对应商品」的存货行，并加预占列。
--
-- 为什么 variant_id / product_id 要可空（对齐 ERP 的 prd_warehouse_sku）：
--   · 真实仓库里经常先有货（客户寄卖、清关到仓、手工补录），商品是几天后才建的；
--   · 现行 NOT NULL 逼着运营先建商品再补库存，于是「不知道是哪个商品」的货只能记账外，
--     结账时对不上 —— 这正是本次要消掉的那笔账；
--   · 认领（把 variant_id 填上）之后，这一行就是商品的库存行，后续链路（预占 / 出库 /
--     流水 / 变体删除守卫）全部照旧，不需要第二套模型。
--
-- 为什么预占列在**这一行**而不在 product_variants：
--   预占是「本仓这批货里有多少已经被订单占住」，维度是 (变体 × 仓)；
--   放在变体上只能表达跨仓总数，发货时无法知道该从哪个仓扣。
--
-- 三量恒等式（照抄 ERP product_inventory_reserve.go 文件头）：
--   on_hand = quantity；reserved = reserved_quantity；available = quantity - reserved_quantity
--   预占 / 释放**只改 reserved_quantity**，不改 quantity、不写流水。
--
-- 联动约束（099 的 UNIQUE (variant_id, warehouse_id)）：PG 的唯一索引把 NULL 视为互不相等，
--   所以放开可空之后同一个仓可以有多条未认领行 —— 这正是需要的（两条货、两个编码），
--   但也意味着「未认领行不重复」不再由 099 兜底。收货端的重复判据改由
--   244 的 UNIQUE (warehouse_id, sku_code) 承担：同仓同裸码插第二行必然撞它。
--
-- 幂等：DROP NOT NULL 重复执行无副作用；ADD COLUMN IF NOT EXISTS；约束用 DO 块吞 duplicate_object。

ALTER TABLE inventory_stocks ALTER COLUMN variant_id  DROP NOT NULL;
ALTER TABLE inventory_stocks ALTER COLUMN product_id  DROP NOT NULL;
ALTER TABLE inventory_stocks ADD COLUMN IF NOT EXISTS reserved_quantity integer NOT NULL DEFAULT 0;

-- reserved 恒非负，且不得超过在库。两条一起把「预占比货还多」挡在 DDL 上：
-- 应用层的容差判断只在并发下作为先手，最终一致性靠这两条。
DO $$
BEGIN
    ALTER TABLE inventory_stocks
        ADD CONSTRAINT ck_inventory_stocks_reserved_nonneg CHECK (reserved_quantity >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE inventory_stocks
        ADD CONSTRAINT ck_inventory_stocks_reserved_le_quantity CHECK (reserved_quantity <= quantity);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- 存量行 reserved_quantity 一律 0（DDL 的 DEFAULT 0 已经覆盖）：
-- 预占是本次才引入的概念，历史订单没有预占事实，不能凭空补一个数 —— 那会让可售数量凭空变少。

-- 未认领行的定位（库存页「未认领」筛选 + 商品侧「从仓库选」的候选集）：
-- 偏索引只覆盖未认领行，比全表索引小一个数量级。
CREATE INDEX IF NOT EXISTS idx_inventory_stocks_unclaimed
    ON inventory_stocks (project_id, warehouse_id, sku_code)
    WHERE variant_id IS NULL;

COMMENT ON COLUMN inventory_stocks.variant_id IS '认领到的商品变体；NULL = 仓库里已有、尚未对应商品的存货行（认领后落值）';
COMMENT ON COLUMN inventory_stocks.product_id IS '认领到的商品；NULL 与 variant_id 同步为空';
COMMENT ON COLUMN inventory_stocks.reserved_quantity IS '本仓预占数量（被未发货订单占住）；可售 = quantity - reserved_quantity；预占/释放只改本列，不改 quantity、不写流水';

-- 外键：variant_id / product_id 原本不带外键（099 只给 variant_id 了 ON DELETE CASCADE，
-- 放开可空后 FK 依然有效 —— NULL 不参与外键校验）。这里不改外键，只留一条提醒：
-- 删除变体时的守卫（product 侧 CountNonZeroStocksByVariant）对未认领行天然不命中，
-- 因为未认领行没有 variant_id。
