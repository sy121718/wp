-- table: product_price_adjustments（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_price_adjustments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    rule_type text NOT NULL,
    rule_params jsonb DEFAULT '{}'::jsonb NOT NULL,
    rounding text DEFAULT 'none'::text NOT NULL,
    scope text NOT NULL,
    target_id uuid,
    filter jsonb DEFAULT '{}'::jsonb NOT NULL,
    variant_count integer DEFAULT 0 NOT NULL,
    changed_count integer DEFAULT 0 NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_price_adjustments_created_at_not_null NOT NULL
);

ALTER TABLE ONLY public.product_price_adjustments FORCE ROW LEVEL SECURITY;

ALTER TABLE public.product_price_adjustments ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.product_price_adjustments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.product_price_adjustments
    ADD CONSTRAINT product_price_adjustments_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_product_price_adjustments_project ON public.product_price_adjustments USING btree (project_id, create_time DESC, id DESC);

-- @phase fk
ALTER TABLE ONLY public.product_price_adjustments
    ADD CONSTRAINT product_price_adjustments_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.product_price_adjustments IS '定价工具调价批次（issue #13；规则算价后落库，留痕）';

COMMENT ON COLUMN public.product_price_adjustments.filter IS '筛选集范围条件（status / keyword / categoryId / brandId / tagId）';

COMMENT ON COLUMN public.product_price_adjustments.operator_id IS '操作人 id（取自会话，测试与脚本路径可为空串）';
