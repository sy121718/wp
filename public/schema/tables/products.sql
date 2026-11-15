-- table: products（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.products (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    subtitle text DEFAULT ''::text NOT NULL,
    description jsonb DEFAULT '{}'::jsonb NOT NULL,
    slug text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    unit text DEFAULT ''::text NOT NULL,
    weight numeric(12,3),
    seo_title text DEFAULT ''::text NOT NULL,
    seo_description text DEFAULT ''::text NOT NULL,
    images jsonb DEFAULT '[]'::jsonb NOT NULL,
    category_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    tag_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    related_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    brand_id uuid,
    default_price numeric(12,2),
    default_compare_price numeric(12,2),
    default_cost_price numeric(12,2),
    default_image text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT products_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT products_updated_at_not_null NOT NULL,
    attribute_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    primary_category_id uuid,
    published_at timestamp with time zone,
    images_alt jsonb DEFAULT '[]'::jsonb NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    CONSTRAINT products_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'archived'::text])))
);

ALTER TABLE ONLY public.products FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_products_project_sku_code ON public.products USING btree (project_id, sku_code) WHERE (sku_code <> ''::text);

CREATE UNIQUE INDEX uq_products_project_slug ON public.products USING btree (project_id, slug);

ALTER TABLE public.products ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.products USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_products_attribute_ids ON public.products USING gin (attribute_ids jsonb_path_ops);

CREATE INDEX idx_products_brand_id ON public.products USING btree (brand_id) WHERE (brand_id IS NOT NULL);

CREATE INDEX idx_products_category_ids ON public.products USING gin (category_ids jsonb_path_ops);

CREATE INDEX idx_products_name_trgm ON public.products USING gin (name ext_shared.gin_trgm_ops);

CREATE INDEX idx_products_primary_category ON public.products USING btree (primary_category_id);

CREATE INDEX idx_products_project_status_sort ON public.products USING btree (project_id, status, sort, create_time, id);

CREATE INDEX idx_products_published_at ON public.products USING btree (project_id, published_at);

CREATE INDEX idx_products_tag_ids ON public.products USING gin (tag_ids jsonb_path_ops);

-- @phase fk
ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES public.product_brands(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_primary_category_id_fkey FOREIGN KEY (primary_category_id) REFERENCES public.product_categories(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.products IS '商品主体（issue #5；价格与库存在变体上）';

COMMENT ON COLUMN public.products.attribute_ids IS '商品引用的属性组 id 数组（issue #7）：只存引用，属性组定义唯一一份，可被多个商品复用';

COMMENT ON COLUMN public.products.primary_category_id IS '商品主分类（issue #10）；其余附属分类在 category_ids';

COMMENT ON COLUMN public.products.published_at IS '上架时间（最近一次进入 published 的时刻；自动标签「新品」规则的判定基准）';

COMMENT ON COLUMN public.products.images_alt IS '图集 alt 文本数组（与 images 逐位对应；元素可为空串；可翻译字段，语境 product.imageAlts）';

COMMENT ON INDEX public.idx_products_brand_id IS '商品按品牌筛选（issue #21 集合源 brandId 维度）';
