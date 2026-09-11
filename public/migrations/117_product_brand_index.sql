-- 117 · 商品按品牌筛选的索引（issue #21）。
--
-- 集合源（#21）新增 brandId 过滤维度，谓词是 products.brand_id = ?。
-- 081 建表时 brand_id 只有外键（REFERENCES product_brands ON DELETE SET NULL），
-- 而 PostgreSQL **不会**为外键自动建索引 —— 商品量上来后「按品牌筛商品」会退化成全表扫描。
--
-- 分类 / 标签的包含查询走 081 已建的 GIN 索引（jsonb_path_ops），本条只补品牌这一列。
-- 用部分索引（WHERE brand_id IS NOT NULL）：「没挂品牌」的查询不走它，
-- 索引体积也跟着变小。

CREATE INDEX IF NOT EXISTS idx_products_brand_id
    ON products(brand_id)
    WHERE brand_id IS NOT NULL;

COMMENT ON INDEX idx_products_brand_id IS '商品按品牌筛选（issue #21 集合源 brandId 维度）';
