-- 120_product_rating.sql
-- 商品评分：**独立评分表**（issue #29 → #30 修正）。
--
-- #29 最初把 rating / rating_count 当成 products 上的两个列。那是「把投影值当存储」：
-- 评分是**明细数据的聚合结果**，存成商品列意味着每次新增一条评分都要回写商品行，
-- 而且商品与评分的关系被压成一个数字、丢掉明细（将来评论域无从接手）。
--
-- 现在改为：
--   · `product_ratings` 明细表（一行 = 一条评分）；
--   · 商品查询经 hasMany 关联 Preload 明细，平均分与条数由明细**投影**算出（不落库）；
--   · 商品表上的 rating / rating_count 两列删除。
--
-- 语义要点不变：**没有评分与 0 分严格区分**。
--   · 无评分 = 没有明细行（投影为空）——排序排最后、不入选最低评分筛选；
--   · 0 分 = 有一条 score=0 的明细。
CREATE TABLE IF NOT EXISTS product_ratings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    product_id uuid NOT NULL,
    score numeric(3,2) NOT NULL CHECK (score >= 0 AND score <= 5),
    -- source 区分来源：manual = 运营补录 / review = 评论域写入（将来）。
    -- 留着它是因为将来评论域接管写入时，历史手工数据仍要能区分出来。
    source text NOT NULL DEFAULT 'manual',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 按商品聚合（AVG / COUNT）与按商品取明细都走这个索引。
CREATE INDEX IF NOT EXISTS idx_product_ratings_product_id ON product_ratings (product_id);
CREATE INDEX IF NOT EXISTS idx_product_ratings_project_id ON product_ratings (project_id);

-- 清掉 #29 加在商品表上的两列（那次的形态已废弃）。
ALTER TABLE products DROP COLUMN IF EXISTS rating;
ALTER TABLE products DROP COLUMN IF EXISTS rating_count;

COMMENT ON TABLE product_ratings IS '商品评分明细（issue #30）：平均分与条数由明细投影，不在商品表上存列'
