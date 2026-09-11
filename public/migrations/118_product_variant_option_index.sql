-- 118_product_variant_option_index.sql
-- 属性值筛选的索引（issue #25）。
--
-- 集合源按 `option.<属性key>=<属性值key>` 筛商品时，每个属性下推一条 EXISTS：
--   EXISTS (SELECT 1 FROM product_variants v
--           WHERE v.product_id = products.id AND v.enabled
--             AND v.option_values @> jsonb_build_object(key, value))
--
-- 谓词形态是 @> 包含判断，故用 jsonb_path_ops：它只索引包含查询需要的结构，
-- 比默认的 jsonb_ops 更小更快，且恰好覆盖 @>（默认 ops 的能力在这个场景里是冗余的）。
-- 变体表按商品查是小结果集，本索引让每个属性的 EXISTS 不必全表扫 option_values。
CREATE INDEX IF NOT EXISTS idx_product_variants_option_values
    ON product_variants USING GIN (option_values jsonb_path_ops);

COMMENT ON INDEX idx_product_variants_option_values IS '属性值筛选（issue #25）：EXISTS 子查询的 @> 包含判断'
