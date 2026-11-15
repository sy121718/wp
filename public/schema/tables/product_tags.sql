-- table: product_tags（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_tags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    kind text DEFAULT 'manual'::text NOT NULL,
    rule_type text DEFAULT ''::text NOT NULL,
    rule_params jsonb DEFAULT '{}'::jsonb NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_tags_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_tags_updated_at_not_null NOT NULL,
    recalc_at timestamp with time zone,
    CONSTRAINT ck_product_tags_rule_shape CHECK ((((kind = 'manual'::text) AND (rule_type = ''::text)) OR ((kind = 'rule'::text) AND (rule_type <> ''::text)))),
    CONSTRAINT product_tags_kind_check CHECK ((kind = ANY (ARRAY['manual'::text, 'rule'::text])))
);

ALTER TABLE ONLY public.product_tags FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_product_tags_project_slug ON public.product_tags USING btree (project_id, slug);

ALTER TABLE public.product_tags ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.product_tags USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.product_tags
    ADD CONSTRAINT product_tags_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.product_tags
    ADD CONSTRAINT product_tags_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.product_tags IS '商品标签（manual 手工 / rule 自动规则，规则参数走 JSON）';

COMMENT ON COLUMN public.product_tags.recalc_at IS '自动标签最近一次按规则重算的时间（手工标签恒为空）';
