-- 244 · 仓库侧成本（(仓库, SKU) 的当前成本价）+ 仓库内 SKU 唯一（批次 A）。
--
-- 口径（用户 2026-09-19 逐条确认，见 docs/14-product-sku-and-cost-model.md §4）：
--   · 成本**写到仓库侧**：同一个 SKU 可以存在于多个仓库 —— 成本与库存一样是
--     (仓库, SKU) 维度，因此这一列在 inventory_stocks 上，不在 products / product_variants；
--   · 仓库侧**只记一个当前成本价**，不做成本流水：覆盖式，最近一次入库（或显式写入）为准；
--     核算归采购侧（可以不走采购流程，由外部核算后导入）；
--   · cost_price 可空：NULL = **尚未核算**。绝不用 0 冒充「未知成本」——
--     0 是合法的显式成本（赠品 / 内部划拨），「未知」与「零成本」是两回事。
--
-- 仓库内唯一：同一仓库不能有重复 SKU（UNIQUE (warehouse_id, sku_code)），
-- 这是「SKU 的身份范围下沉到仓库」的直接结果（docs/14 §4.7 / §7）。
-- 为什么**先扫再建**：存量库若已有同仓同 SKU 的多行，直接建唯一索引只会抛一个
-- 没有上下文的 23505（duplicate key value violates unique constraint）—— 事后根本
-- 看不出是哪两行。这里先聚合重复组，带 warehouse_id / sku_code / 行 id 样例 RAISE
-- EXCEPTION：迁移整体失败、数据一字不动，让人拿着具体行去人工处理。既不静默丢数据，
-- 也不需要对着索引名猜。
--
-- 幂等：ADD COLUMN IF NOT EXISTS；约束用 DO 块吞 duplicate_object / duplicate_table
--（重跑不会失败；语句自带异常处理的子事务，失败不会污染外层）。

ALTER TABLE inventory_stocks ADD COLUMN IF NOT EXISTS cost_price numeric(12,2);

-- 精度对齐既有价格列（product_variants.cost_price / inventory_sources.settle_price
-- 同为 numeric(12,2)）：成本与它们是同一个量纲，字符串口径必须一致。
COMMENT ON COLUMN inventory_stocks.cost_price IS '当前成本价（(仓库, SKU) 一个值，覆盖式，最近一次入库 / 显式写入为准）；NULL = 尚未核算，0 是合法的显式成本';

DO $$
DECLARE
    dup_groups integer;
    dup_rows   integer;
    samples    text;
BEGIN
    SELECT COUNT(*), COALESCE(SUM(cnt), 0)
      INTO dup_groups, dup_rows
      FROM (
          SELECT COUNT(*) AS cnt
            FROM inventory_stocks
           GROUP BY warehouse_id, sku_code
          HAVING COUNT(*) > 1
      ) g;

    IF dup_groups > 0 THEN
        SELECT string_agg(
                   format('warehouse_id=%s sku_code=%L rows=%s ids=%s', warehouse_id, sku_code, cnt, ids),
                   E'\n' ORDER BY warehouse_id, sku_code)
          INTO samples
          FROM (
              SELECT warehouse_id, sku_code, COUNT(*) AS cnt,
                     string_agg(id::text, ',' ORDER BY id) AS ids
                FROM inventory_stocks
               GROUP BY warehouse_id, sku_code
              HAVING COUNT(*) > 1
          ) d;

        RAISE EXCEPTION 'inventory_stocks 存在同仓同 SKU 的多行（% 组 / % 行），无法建立 UNIQUE (warehouse_id, sku_code)。请先人工处理下列行后重跑迁移：%',
            dup_groups, dup_rows, samples
            USING ERRCODE = 'unique_violation';
    END IF;
END $$;

DO $$
BEGIN
    ALTER TABLE inventory_stocks
        ADD CONSTRAINT uq_inventory_stocks_warehouse_sku UNIQUE (warehouse_id, sku_code);
EXCEPTION
    -- 约束已存在（重复执行 / 迁移判定漏判）：什么都不做。
    WHEN duplicate_object THEN NULL;
    -- 同名唯一索引已存在（例如手工建过索引而不是约束）：同样视为已满足。
    WHEN duplicate_table THEN NULL;
END $$;
