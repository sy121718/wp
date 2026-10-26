-- 637 · 变体侧持有「本变体引用了哪些仓库货行」（JSON 关联，取代认领）。
--
-- 为什么把关联放到商品变体上，而不是库存行上：
--   · 认领模型（624）是**一对一**：一条库存行的 variant_id 决定它属于谁，认领是单向动作、
--     不能自动合并。真实仓库里同一个仓的一条货（例如「红色 · 大号」这个仓库 SKU）常常要同时
--     供**多个**商品变体卖（组合、套装、不同站点的同款），一对一表达不了，只能拆成两份货
--     —— 于是同一批货在账上变成两行，数量对不上。
--   · 放到变体侧就是多对多：一个变体记住它引用了哪几条货行；一条货行可以被多个变体引用。
--     这与 dy_erp 的 prd_sku_warehouse_sku 同构，只是**不建关联表**（关联是本变体的属性，
--     反查走 GIN 索引，见下）。
--
-- 为什么元素里只存标识、不存名称快照：
--   名称会漂移（商品改名 / 仓库改名 / 外码重登），存快照等于在两张表之间留一份会各自演化的账。
--   元素形状（固定四项，缺一不可）：
--     {"stockId":"<inventory_stocks.id>","warehouseId":"<仓 id>",
--      "warehouseCode":"<仓短码大写>","skuCode":"<该仓裸码>"}
--   stockId 是**定位键**（编码可以改，行 id 不会）；warehouseId 让它能不过查就分组；
--   warehouseCode / skuCode 是给「货行被删了但引用还在」的悬空态留的只读线索
--   （守卫与页面都靠它说清楚「引用的是哪一条」），不参与任何判定。
--
-- 回填口径：存量一行 `variant_id = V AND warehouse_id = W` 的库存行，就往变体 V 的
-- warehouse_skus 追加一条指向它的元素。**未认领行（variant_id IS NULL）不回填** ——
-- 它们本来就不属于任何变体，是「仓库里有货、还没建商品」的那批，按新模型它们等着被引用。
--
-- 回填后 inventory_stocks.variant_id / product_id 仍然保留：这一批只把关联搬到变体侧并停掉
-- 认领动作，库存寻址（预占 / 扣减 / 调拨按「变体 × 仓」定位）留给下一批改。641 会删掉
-- (variant_id, warehouse_id) 这条唯一键，两列自此退化成「写入当时的线索」（不再唯一、
-- 不参与定位）；读路径一律按货行 id 或 (仓, 仓库 SKU) 走，见迁移 640 / 641。
--
-- 幂等：ADD COLUMN IF NOT EXISTS；回填用 `WHERE warehouse_skus = '[]'` 只碰空数组的行，
-- 重跑不会把同一条元素追加两次。

ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS warehouse_skus jsonb;
UPDATE product_variants SET warehouse_skus = '[]'::jsonb WHERE warehouse_skus IS NULL;
ALTER TABLE product_variants ALTER COLUMN warehouse_skus SET DEFAULT '[]'::jsonb;
ALTER TABLE product_variants ALTER COLUMN warehouse_skus SET NOT NULL;

-- 形状守卫：必须是数组，且每个元素都有非空的 stockId 与 warehouseId。
-- 单靠服务层守不住 —— JSONB 可以被任何一条 UPDATE 写坏，而坏掉的形状在读路径上表现为
-- 「这个变体的库存凭空变少」（引用解析不到行），事后只能靠逐行比对发现。
--
-- 用 jsonb_path_exists 而不是 jsonb_array_elements 子查询：CHECK 约束里**不许出现子查询**
-- （SQLSTATE 0A000），而 jsonb_path_exists 是 immutable 标量函数，谓词写在一段 jsonpath 里。
-- jsonpath 读法：`$[*]` 迭代全部元素，`? (...)` 留下满足括号内条件的那些 ——
-- exists() 为假（缺键）或值为空串都算坏形状。
DO $$
BEGIN
    ALTER TABLE product_variants
        ADD CONSTRAINT product_variants_warehouse_skus_shape_check
        CHECK (
            jsonb_typeof(warehouse_skus) = 'array'
            AND NOT jsonb_path_exists(
                warehouse_skus,
                '$[*] ? (!exists($.stockId) || !exists($.warehouseId)
                      || $.stockId == "" || $.warehouseId == "")'
            )
        );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- 反查索引：回答「全库还有谁引用着这条货行」（删货行 / 删仓 / 死引用巡检）。
-- jsonb_path_ops 比默认的 jsonb_ops 小一个数量级，且 `@>` 是这里唯一的查询形态。
CREATE INDEX IF NOT EXISTS idx_product_variants_warehouse_skus
    ON product_variants USING gin (warehouse_skus jsonb_path_ops);

-- 回填：一条已认领库存行 → 对应变体的一条引用元素。
-- 按变体聚合后**一次**更新（`GROUP BY` 在子查询里），避免同一变体多仓时被更新多次
-- —— 后一次会覆盖前一次，只剩最后一个仓的引用。
UPDATE product_variants v
   SET warehouse_skus = agg.items,
       update_time = now()
  FROM (
      SELECT s.variant_id AS variant_id,
             jsonb_agg(jsonb_build_object(
                 'stockId',       s.id,
                 'warehouseId',   s.warehouse_id,
                 'warehouseCode', w.code,
                 'skuCode',       s.sku_code
             ) ORDER BY w.sort ASC, w.code ASC, s.sku_code ASC) AS items
        FROM inventory_stocks s
        JOIN inventory_warehouses w ON w.id = s.warehouse_id
       WHERE s.variant_id IS NOT NULL
       GROUP BY s.variant_id
  ) agg
 WHERE v.id = agg.variant_id
   AND v.warehouse_skus = '[]'::jsonb;

COMMENT ON COLUMN product_variants.warehouse_skus IS
    '本变体引用的仓库货行（JSON 数组，元素 {stockId,warehouseId,warehouseCode,skuCode}）；多对多，取代 624 的认领；反查走 idx_product_variants_warehouse_skus';
