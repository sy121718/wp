-- table: product_ratings（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_ratings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    product_id uuid NOT NULL,
    score numeric(3,2) NOT NULL,
    source text DEFAULT 'manual'::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_ratings_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_ratings_updated_at_not_null NOT NULL,
    CONSTRAINT product_ratings_score_check CHECK (((score >= (0)::numeric) AND (score <= (5)::numeric)))
);

ALTER TABLE ONLY public.product_ratings FORCE ROW LEVEL SECURITY;

ALTER TABLE public.product_ratings ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.product_ratings USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.product_ratings
    ADD CONSTRAINT product_ratings_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_product_ratings_product_id ON public.product_ratings USING btree (product_id);

CREATE INDEX idx_product_ratings_project_id ON public.product_ratings USING btree (project_id);

-- @phase comment
COMMENT ON TABLE public.product_ratings IS '商品评分明细（issue #30）：平均分与条数由明细投影，不在商品表上存列';
