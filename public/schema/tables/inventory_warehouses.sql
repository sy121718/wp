-- table: inventory_warehouses（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_warehouses (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_warehouses_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_warehouses_updated_at_not_null NOT NULL,
    type text DEFAULT 'self'::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    deduct_priority integer DEFAULT 100 NOT NULL,
    CONSTRAINT inventory_warehouses_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT inventory_warehouses_type_check CHECK ((type = ANY (ARRAY['self'::text, 'third_party'::text, 'virtual'::text])))
);

ALTER TABLE ONLY public.inventory_warehouses FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_warehouses_project_code ON public.inventory_warehouses USING btree (project_id, upper(code));

CREATE UNIQUE INDEX uq_inventory_warehouses_project_default ON public.inventory_warehouses USING btree (project_id) WHERE is_default;

ALTER TABLE public.inventory_warehouses ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_warehouses USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_warehouses_deduct ON public.inventory_warehouses USING btree (project_id, deduct_priority, code);

-- @phase fk
ALTER TABLE ONLY public.inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.inventory_warehouses IS '仓库实体（issue #15；短码 / 名称 / 状态 / 默认仓）';

COMMENT ON COLUMN public.inventory_warehouses.code IS '仓库短码（工程内唯一大写），SKU 编码前缀';

COMMENT ON COLUMN public.inventory_warehouses.is_default IS '默认仓标记：每工程至多一行，「未指定仓库」时的兜底';

COMMENT ON COLUMN public.inventory_warehouses.deduct_priority IS '出库扣减优先级：值小先扣；仅在未显式指定仓库时生效。默认 100 = 与未设置等价';
