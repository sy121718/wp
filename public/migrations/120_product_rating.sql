-- 120_product_rating.sql
-- 商品评分（issue #29）。
--
-- 本期只落**商品级评分字段**（rating + rating_count），不做评论域：
-- 列表要的只是「按评分排 / 按评分筛」，而评论域（提交、审核、聚合、重算）是独立一大块。
-- 字段与接口先定死：将来接评论域时改为由聚合任务写这两个值即可，
-- 集合源、排序键、筛选维度、后台表单都不用动。
--
-- 语义要点：**NULL 与 0 分严格区分**。
--   · rating IS NULL = 「尚无评分」——列表按评分排序时排最后，不按 0 分参与比较；
--   · rating = 0   = 「评分为 0」，与前者不是一回事。
ALTER TABLE products ADD COLUMN IF NOT EXISTS rating numeric(3,2);
ALTER TABLE products ADD COLUMN IF NOT EXISTS rating_count integer NOT NULL DEFAULT 0;

-- 约束用 DO 块包住（幂等重跑：约束已存在时不重复添加，也不报错）。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'products_rating_range_check') THEN
        ALTER TABLE products ADD CONSTRAINT products_rating_range_check
            CHECK (rating IS NULL OR (rating >= 0 AND rating <= 5));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'products_rating_count_check') THEN
        ALTER TABLE products ADD CONSTRAINT products_rating_count_check CHECK (rating_count >= 0);
    END IF;
END $$;

COMMENT ON COLUMN products.rating IS '商品评分 0~5（NULL = 尚无评分，与 0 分严格区分）；issue #29';
COMMENT ON COLUMN products.rating_count IS '评价数量（默认 0）；issue #29'
