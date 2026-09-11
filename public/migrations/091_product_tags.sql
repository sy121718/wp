-- 091 · 商品标签规则化（issue #11）。
--
-- 标签表 product_tags 由 081 建好（kind=manual/rule + rule_type + rule_params JSONB），
-- 本迁移只补「规则化」必需的三个东西：
--   ① products.published_at —— 「上架 N 天内即新品」的判定基准。上架时间此前没有落列，
--      拿 created_at 顶替会把「建了草稿很久才上架」的商品漏判成新品；
--   ② product_tags.recalc_at —— 自动标签最近一次按规则重算的时间（重算时机在后台可见）；
--   ③ ck_product_tags_rule_shape —— 形状约束：manual 不许带规则、rule 必须带规则类型。
--      （内置规则类型与参数白名单的合法性判定在 service，见 product_tag_rule.go；
--      列级 CHECK 只兜底「形状」，避免每加一个规则类型就要改一次 DDL。）
--
-- published_at 为历史已上架商品回填 updated_at：本列此前不存在，没有更准确的来源，
-- 回填保证规则在迁移后立刻有可判定的数据（不回填则全部历史商品永远不算新品）。

ALTER TABLE products ADD COLUMN IF NOT EXISTS published_at timestamptz NULL;
ALTER TABLE product_tags ADD COLUMN IF NOT EXISTS recalc_at timestamptz NULL;

UPDATE products SET published_at = updated_at WHERE status = 'published' AND published_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_products_published_at ON products(project_id, published_at);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'ck_product_tags_rule_shape' AND conrelid = 'product_tags'::regclass
    ) THEN
        ALTER TABLE product_tags ADD CONSTRAINT ck_product_tags_rule_shape CHECK (
            (kind = 'manual' AND rule_type = '') OR (kind = 'rule' AND rule_type <> '')
        );
    END IF;
END $$;

COMMENT ON COLUMN products.published_at IS '上架时间（最近一次进入 published 的时刻；自动标签「新品」规则的判定基准）';
COMMENT ON COLUMN product_tags.recalc_at IS '自动标签最近一次按规则重算的时间（手工标签恒为空）';
