-- 088 · 商品分类 / 品牌与商品关联补齐（issue #10）。
--
-- 081 已建好 product_categories（树形自引用）与 product_brands 两张表，本迁移只补
-- 「商品 → 主分类」这一列：
--   · 附属分类沿用 081 的 products.category_ids（JSONB 数组，多分类）；
--   · 主分类需要「只有一个 + 可被反查 + 分类被删时自动解绑」，故落成真列 + 外键。
--
-- slug 唯一（工程内）、父子层级、排序与 SEO 字段在 081 已就位，本迁移不重复定义。
-- 注册：public/migrations/register.go（Migration 088-product-taxonomy）。

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS primary_category_id uuid NULL
        REFERENCES product_categories(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_products_primary_category
    ON products(primary_category_id);
-- 分类树按父级取子节点、后台按工程列树，两处都需要索引覆盖。
CREATE INDEX IF NOT EXISTS idx_product_categories_parent
    ON product_categories(parent_id);
CREATE INDEX IF NOT EXISTS idx_product_categories_project_sort
    ON product_categories(project_id, sort);
CREATE INDEX IF NOT EXISTS idx_product_brands_project_sort
    ON product_brands(project_id, sort);

COMMENT ON COLUMN products.primary_category_id IS '商品主分类（issue #10）；其余附属分类在 category_ids';
