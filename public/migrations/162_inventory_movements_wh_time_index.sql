-- 162 · 库存流水按仓库 + 时间索引（IDX-007）。
--
-- 后台「某仓库的变动历史」按 warehouse_id 过滤、created_at 倒序分页；
-- 102 已有 project_id / variant_id 维度索引，缺仓库维度会导致大表 seq scan。

CREATE INDEX IF NOT EXISTS idx_inventory_movements_wh_time
    ON inventory_stock_movements (warehouse_id, created_at DESC);

COMMENT ON INDEX idx_inventory_movements_wh_time IS '仓库维度流水历史查询（IDX-007）';
