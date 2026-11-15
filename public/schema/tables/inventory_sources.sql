-- table: inventory_sources（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_sources (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    type text DEFAULT 'external'::text NOT NULL,
    related_party boolean DEFAULT false NOT NULL,
    settle_price numeric(12,2),
    status text DEFAULT 'active'::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_sources_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_sources_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_sources_config_object_check CHECK ((jsonb_typeof(config) = 'object'::text)),
    CONSTRAINT inventory_sources_internal_related_check CHECK (((type = 'external'::text) OR related_party)),
    CONSTRAINT inventory_sources_settle_price_check CHECK (((settle_price IS NULL) OR ((type = 'internal'::text) AND (settle_price >= (0)::numeric)))),
    CONSTRAINT inventory_sources_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT inventory_sources_type_check CHECK ((type = ANY (ARRAY['external'::text, 'internal'::text])))
);

ALTER TABLE ONLY public.inventory_sources FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_sources_project_code ON public.inventory_sources USING btree (project_id, upper(code));

ALTER TABLE public.inventory_sources ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_sources USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_sources
    ADD CONSTRAINT inventory_sources_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_sources_project_party ON public.inventory_sources USING btree (project_id, related_party);

CREATE INDEX idx_inventory_sources_project_type ON public.inventory_sources USING btree (project_id, type);

-- @phase fk
ALTER TABLE ONLY public.inventory_sources
    ADD CONSTRAINT inventory_sources_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.inventory_sources IS '货源（供应商 / 集团内关联公司 / 自家工厂，issue #17）';

COMMENT ON COLUMN public.inventory_sources.type IS 'external 外部供应商 / internal 集团内（关联公司、自家工厂）';

COMMENT ON COLUMN public.inventory_sources.related_party IS '关联方标志：关联交易报表的判据；内部货源恒为真';

COMMENT ON COLUMN public.inventory_sources.settle_price IS '内部结算价：自产商品的成本口径（外部供应商不得有值）';

COMMENT ON COLUMN public.inventory_sources.config IS '异构对接扩展信息（JSON 对象）：不同来源的字段形状各不相同';
