-- 095 · 商品定价工具留痕（issue #13）。
--
-- 定价工具是「改价动作」而不是「构建期计算」：四种规则（成本乘倍数 / 成本加价 /
-- 目标毛利率 / 统一售价）算出的售价直接写回 product_variants.price，
-- 构建期（实体类型解析器 / 集合源）读到的就是落库后的确定值 —— 本迁移只新增留痕台账，
-- 不新增任何构建期读取路径。
--
-- 两张表：
--   product_price_adjustments      调价批次（规则 / 尾数处理 / 作用范围 / 命中与改动数 / 操作人）
--   product_price_adjustment_items 逐变体明细（原价 → 新价），批次删除时级联删除
--
-- 「改动有留痕」是本迁移的全部理由：只改价格不记账，事后无法回答「这个价是谁按哪条规则改的」。
-- 明细不建外键指向 product_variants（与 081 的关联列精简原则一致：变体删除后
-- 留痕仍需可读，历史记录只存 id 与当时的 SKU 编码快照）。

CREATE TABLE IF NOT EXISTS product_price_adjustments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects(id),
    rule_type      text NOT NULL,
    rule_params    jsonb NOT NULL DEFAULT '{}'::jsonb,
    rounding       text NOT NULL DEFAULT 'none',
    scope          text NOT NULL,
    target_id      uuid NULL,
    filter         jsonb NOT NULL DEFAULT '{}'::jsonb,
    variant_count  integer NOT NULL DEFAULT 0,
    changed_count  integer NOT NULL DEFAULT 0,
    note           text NOT NULL DEFAULT '',
    operator_id    text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_product_price_adjustments_project
    ON product_price_adjustments(project_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS product_price_adjustment_items (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    adjustment_id  uuid NOT NULL REFERENCES product_price_adjustments(id) ON DELETE CASCADE,
    product_id     uuid NOT NULL,
    variant_id     uuid NOT NULL,
    sku_code       text NOT NULL DEFAULT '',
    old_price      numeric(12,2) NOT NULL,
    new_price      numeric(12,2) NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_product_price_adjustment_items_adjustment
    ON product_price_adjustment_items(adjustment_id);
CREATE INDEX IF NOT EXISTS idx_product_price_adjustment_items_variant
    ON product_price_adjustment_items(variant_id, created_at DESC);

COMMENT ON TABLE product_price_adjustments IS '定价工具调价批次（issue #13；规则算价后落库，留痕）';
COMMENT ON TABLE product_price_adjustment_items IS '调价批次逐变体明细（原价 → 新价）';
COMMENT ON COLUMN product_price_adjustments.filter IS '筛选集范围条件（status / keyword / categoryId / brandId / tagId）';
COMMENT ON COLUMN product_price_adjustments.operator_id IS '操作人 id（取自会话，测试与脚本路径可为空串）';
