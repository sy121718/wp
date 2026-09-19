-- 238 · 商品类型：variant（常规变体商品）/ bundle（捆绑容器）。
--
-- 为什么现在才补这一列：捆绑品此前只是「bundle_items 非空的商品」，系统分不清
-- 「主体卖自己的 SKU」与「容器卖组合」。后果是创建路径无差别地给它生成了一个价 0 的
-- 首个变体，而价格区间（applyPriceRange）只从变体派生 —— 捆绑商品在列表里显示 0.00。
-- 有了 type：bundle 不再生成首个变体，价格只取容器价（products.default_price）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS；约束用 DO 块吞 duplicate_object；
-- 存量回填按「bundle_items.options 是非空数组」判定（空配置的商品不算捆绑品）。
ALTER TABLE products ADD COLUMN IF NOT EXISTS type text NOT NULL DEFAULT 'variant';

DO $$ BEGIN
    ALTER TABLE products ADD CONSTRAINT products_type_check CHECK (type IN ('variant','bundle'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

UPDATE products SET type = 'bundle'
 WHERE type <> 'bundle'
   AND jsonb_typeof(bundle_items) = 'object'
   AND jsonb_typeof(bundle_items -> 'options') = 'array'
   AND jsonb_array_length(bundle_items -> 'options') > 0;
