-- 640 · 流水加货行指针（stock_id）。
--
-- 637 把「变体 ↔ 货行」的关联搬到了商品侧（product_variants.warehouse_skus，多对多）。
-- 下一步要把库存寻址从「(变体, 仓)」改成「货行 id」——因为同一变体在同一个仓
-- 可以对应多条货（不同批次 / 自有码与客户码并存），旧的一对一键表达不了。
--
-- 台账要能回答「这条变动动的是哪一条货行」，而那时候 (变体, 仓) 已不再唯一，
-- 所以流水必须先记下行 id。本迁移只做这一件事（纯加法）：
--   1. 加 stock_id 列（可空）；
--   2. 按 (variant_id, warehouse_id) 回填历史（那对值在当时唯一，回填是确定的）；
--   3. 外键 ON DELETE SET NULL —— 货行被删是合法的，历史流水不该因此消失；
--   4. 行级索引（「这条货的全部流水」是台账与对账的主查询）。
--
-- 删 inventory_stocks 上的归属两列与寻址改造留给下一批：结构变更与读路径改造
-- 分两次落地，中间态（列在、但不再被新代码写）是可运行的。

ALTER TABLE inventory_stock_movements ADD COLUMN IF NOT EXISTS stock_id uuid;

-- 回填：当时 (variant_id, warehouse_id) 唯一，所以这条 JOIN 每条流水至多命中一行。
-- 只碰 stock_id 为空的行，重跑幂等。
UPDATE inventory_stock_movements mv
   SET stock_id = s.id
  FROM inventory_stocks s
 WHERE mv.stock_id IS NULL
   AND s.variant_id = mv.variant_id
   AND s.warehouse_id = mv.warehouse_id;

DO $$
BEGIN
    ALTER TABLE inventory_stock_movements
        ADD CONSTRAINT inventory_stock_movements_stock_id_fkey
        FOREIGN KEY (stock_id) REFERENCES inventory_stocks(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_inventory_movements_stock_time
    ON inventory_stock_movements (stock_id, create_time DESC);

COMMENT ON COLUMN inventory_stock_movements.stock_id IS
    '这条变动动的是哪一条库存货行；货行被删时置空，历史仍可读';
