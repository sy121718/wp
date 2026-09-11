-- 081 · 商品域表结构（issue #5 / T3a，spec docs 议题 #2 的数据结构精简原则）
--
-- 六张表：商品主体 / 变体 / 属性（组+值合一）/ 分类 / 品牌 / 标签（含自动规则）。
--
-- 精简原则（spec §数据结构精简原则）：
--   · 关联关系走 JSON 列，不建关联表：图片 URL、分类 id、标签 id、关联商品 id、捆绑 BOM；
--   · 属性组与属性值合一张表，值以 JSON 承载；标签与规则合一张表，规则参数以 JSON 承载。
-- 代价（已在 spec 中记录并接受）：无数据库级引用完整性、反查依赖 GIN 索引。
--
-- 库存真源在仓库模块（后续票），本表的 stock_total 只是列表展示用的冗余缓存。
-- 注册：public/migrations/register.go（Migration 081-product-tables）。

-- 1) 分类（树形自引用）
CREATE TABLE IF NOT EXISTS product_categories (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL REFERENCES projects(id),
    parent_id        uuid NULL REFERENCES product_categories(id) ON DELETE SET NULL,
    name             text NOT NULL,
    slug             text NOT NULL,
    description      text NOT NULL DEFAULT '',
    image            text NOT NULL DEFAULT '',
    seo_title        text NOT NULL DEFAULT '',
    seo_description  text NOT NULL DEFAULT '',
    sort             int  NOT NULL DEFAULT 0,
    metadata         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_categories_project_slug ON product_categories(project_id, slug);

-- 2) 品牌
CREATE TABLE IF NOT EXISTS product_brands (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL REFERENCES projects(id),
    name             text NOT NULL,
    slug             text NOT NULL,
    logo             text NOT NULL DEFAULT '',
    description      text NOT NULL DEFAULT '',
    seo_title        text NOT NULL DEFAULT '',
    seo_description  text NOT NULL DEFAULT '',
    sort             int  NOT NULL DEFAULT 0,
    metadata         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_brands_project_slug ON product_brands(project_id, slug);

-- 3) 属性（组 + 值合一；is_variation 决定是否参与变体笛卡尔积）
CREATE TABLE IF NOT EXISTS product_attributes (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid NOT NULL REFERENCES projects(id),
    key           text NOT NULL,
    name          text NOT NULL,
    is_variation  boolean NOT NULL DEFAULT true,
    sort          int  NOT NULL DEFAULT 0,
    values        jsonb NOT NULL DEFAULT '[]'::jsonb,
    metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_attributes_project_key ON product_attributes(project_id, key);

-- 4) 标签（manual 手工 / rule 自动，规则类型 + 参数走 JSON）
CREATE TABLE IF NOT EXISTS product_tags (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id),
    name         text NOT NULL,
    slug         text NOT NULL,
    kind         text NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual', 'rule')),
    rule_type    text NOT NULL DEFAULT '',
    rule_params  jsonb NOT NULL DEFAULT '{}'::jsonb,
    sort         int  NOT NULL DEFAULT 0,
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_tags_project_slug ON product_tags(project_id, slug);

-- 5) 商品主体（图片 / 分类 / 标签 / 关联 / 捆绑 BOM 全走 JSON；价格在变体上）
CREATE TABLE IF NOT EXISTS products (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id             uuid NOT NULL REFERENCES projects(id),
    name                   text NOT NULL,
    subtitle               text NOT NULL DEFAULT '',
    description            jsonb NOT NULL DEFAULT '{}'::jsonb,
    slug                   text NOT NULL,
    status                 text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    sort                   int  NOT NULL DEFAULT 0,
    unit                   text NOT NULL DEFAULT '',
    weight                 numeric(12,3) NULL,
    seo_title              text NOT NULL DEFAULT '',
    seo_description        text NOT NULL DEFAULT '',
    images                 jsonb NOT NULL DEFAULT '[]'::jsonb,
    category_ids           jsonb NOT NULL DEFAULT '[]'::jsonb,
    tag_ids                jsonb NOT NULL DEFAULT '[]'::jsonb,
    related_ids            jsonb NOT NULL DEFAULT '[]'::jsonb,
    bundle_items           jsonb NOT NULL DEFAULT '[]'::jsonb,
    brand_id               uuid NULL REFERENCES product_brands(id) ON DELETE SET NULL,
    default_price          numeric(12,2) NULL,
    default_compare_price  numeric(12,2) NULL,
    default_cost_price     numeric(12,2) NULL,
    default_image          text NOT NULL DEFAULT '',
    metadata               jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_products_project_slug ON products(project_id, slug);
CREATE INDEX IF NOT EXISTS idx_products_category_ids ON products USING gin (category_ids jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_products_tag_ids ON products USING gin (tag_ids jsonb_path_ops);

-- 6) 变体（一行一个；规格组合与 SKU 编码在商品内唯一）
CREATE TABLE IF NOT EXISTS product_variants (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id       uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sku_code         text NOT NULL,
    barcode          text NOT NULL DEFAULT '',
    price            numeric(12,2) NOT NULL DEFAULT 0,
    compare_price    numeric(12,2) NULL,
    cost_price       numeric(12,2) NULL,
    image            text NOT NULL DEFAULT '',
    option_values    jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled          boolean NOT NULL DEFAULT true,
    sort             int NOT NULL DEFAULT 0,
    stock_total      integer NOT NULL DEFAULT 0,
    stock_synced_at  timestamptz NULL,
    metadata         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, sku_code),
    UNIQUE (product_id, option_values)
);
CREATE INDEX IF NOT EXISTS idx_product_variants_product ON product_variants(product_id);

COMMENT ON TABLE products IS '商品主体（issue #5；价格与库存在变体上）';
COMMENT ON TABLE product_variants IS '商品变体 / SKU（一行一个；stock_total 为仓库真源的冗余缓存）';
COMMENT ON TABLE product_attributes IS '商品属性（组 + 值合一，值走 JSON；is_variation 决定是否参与变体生成）';
COMMENT ON TABLE product_categories IS '商品分类（树形）';
COMMENT ON TABLE product_brands IS '商品品牌';
COMMENT ON TABLE product_tags IS '商品标签（manual 手工 / rule 自动规则，规则参数走 JSON）';