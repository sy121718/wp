-- 083 · 修正 product_variants 的规格组合唯一约束（issue #5 收尾）
--
-- 081 里写的是表级 UNIQUE (product_id, option_values)，它把「空组合 {}」也当成一个
-- 可唯一的值 —— 于是同一商品下第二个没有规格的变体（手工新增、或首个变体）会直接
-- 撞唯一约束。规格组合唯一只应对**有规格**的变体生效。
--
-- 幂等：DROP CONSTRAINT IF EXISTS + CREATE UNIQUE INDEX IF NOT EXISTS。
-- 注册：public/migrations/register.go（Migration 083-product-variant-options-unique）。

ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_product_id_option_values_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_product_variants_product_options
    ON product_variants(product_id, option_values)
    WHERE option_values <> '{}'::jsonb;