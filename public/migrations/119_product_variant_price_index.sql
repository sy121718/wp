-- 119_product_variant_price_index.sql
-- 价格区间筛选与价格排序的索引（issue #28）。
--
-- 集合源的价格维度下推 EXISTS（价格在变体上，不是商品列）：
--   EXISTS (SELECT 1 FROM product_variants v
--           WHERE v.product_id = products.id AND v.enabled AND v.price >= ? AND v.price <= ?)
-- 价格排序则取 MIN(v.price)（投影别名 min_price）——两者都是「按商品找变体、按价格过滤」。
--
-- 部分索引只覆盖 enabled = true：筛选与排序都只认启用变体（下架规格的价格不该参与），
-- 索引因此比全表索引更小，且与谓词完全对齐。
CREATE INDEX IF NOT EXISTS idx_product_variants_price_enabled
    ON product_variants (product_id, price) WHERE enabled;

COMMENT ON INDEX idx_product_variants_price_enabled IS '价格区间筛选与价格排序（issue #28）：只覆盖启用变体'
