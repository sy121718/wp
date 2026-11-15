-- table: product_categories（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_categories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    parent_id uuid,
    name text NOT NULL,
    slug text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    image text DEFAULT ''::text NOT NULL,
    seo_title text DEFAULT ''::text NOT NULL,
    seo_description text DEFAULT ''::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_categories_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_categories_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.product_categories FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_product_categories_project_slug ON public.product_categories USING btree (project_id, slug);

ALTER TABLE public.product_categories ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.product_categories USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_product_categories_parent ON public.product_categories USING btree (parent_id);

CREATE INDEX idx_product_categories_project_sort ON public.product_categories USING btree (project_id, sort);

-- @phase fk
ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.product_categories(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.product_categories IS '商品分类（树形）';
