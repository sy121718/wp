-- table: inventory_stock_movements_default（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_stock_movements_default (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid,
    variant_id uuid,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    stock_id uuid,
    stock_name text DEFAULT ''::text CONSTRAINT inventory_stock_movements_stock_name_not_null NOT NULL,
    stock_image_url text DEFAULT ''::text CONSTRAINT inventory_stock_movements_stock_image_url_not_null NOT NULL,
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_default FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_default DEFAULT;

ALTER TABLE public.inventory_stock_movements_default ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_stock_movements_default USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_stock_movements_default
    ADD CONSTRAINT inventory_stock_movements_default_pkey PRIMARY KEY (id, create_time);

-- @phase idx
CREATE INDEX inventory_stock_movements_default_batch_id_created_at_idx ON public.inventory_stock_movements_default USING btree (batch_id, create_time DESC);

CREATE INDEX inventory_stock_movements_default_project_id_created_at_idx ON public.inventory_stock_movements_default USING btree (project_id, create_time DESC);

CREATE INDEX inventory_stock_movements_default_reason_code_idx ON public.inventory_stock_movements_default USING btree (reason_code);

CREATE INDEX inventory_stock_movements_default_stock_id_create_time_idx ON public.inventory_stock_movements_default USING btree (stock_id, create_time DESC);

CREATE INDEX inventory_stock_movements_default_variant_id_created_at_idx ON public.inventory_stock_movements_default USING btree (variant_id, create_time DESC);

CREATE INDEX inventory_stock_movements_default_warehouse_id_created_at_idx ON public.inventory_stock_movements_default USING btree (warehouse_id, create_time DESC);

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_default_batch_id_created_at_idx;

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_default_pkey;

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_default_project_id_created_at_idx;

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_default_reason_code_idx;

ALTER INDEX public.idx_inventory_movements_stock_time ATTACH PARTITION public.inventory_stock_movements_default_stock_id_create_time_idx;

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_default_variant_id_created_at_idx;

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_default_warehouse_id_created_at_idx;
