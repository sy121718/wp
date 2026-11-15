-- table: inventory_stock_movements（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_stock_movements (
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
    stock_name text DEFAULT ''::text NOT NULL,
    stock_image_url text DEFAULT ''::text NOT NULL,
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
)
PARTITION BY RANGE (create_time);

ALTER TABLE ONLY public.inventory_stock_movements FORCE ROW LEVEL SECURITY;

ALTER TABLE public.inventory_stock_movements ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_stock_movements USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_pkey1 PRIMARY KEY (id, create_time);

-- @phase idx
CREATE INDEX idx_inventory_movements_batch ON ONLY public.inventory_stock_movements USING btree (batch_id, create_time DESC);

CREATE INDEX idx_inventory_movements_project_time ON ONLY public.inventory_stock_movements USING btree (project_id, create_time DESC);

CREATE INDEX idx_inventory_movements_reason ON ONLY public.inventory_stock_movements USING btree (reason_code);

CREATE INDEX idx_inventory_movements_stock_time ON ONLY public.inventory_stock_movements USING btree (stock_id, create_time DESC);

CREATE INDEX idx_inventory_movements_variant_time ON ONLY public.inventory_stock_movements USING btree (variant_id, create_time DESC);

CREATE INDEX idx_inventory_movements_wh_time ON ONLY public.inventory_stock_movements USING btree (warehouse_id, create_time DESC);

-- @phase fk
ALTER TABLE public.inventory_stock_movements
    ADD CONSTRAINT fk_inventory_movements_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;

ALTER TABLE public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_stock_id_fkey FOREIGN KEY (stock_id) REFERENCES public.inventory_stocks(id) ON DELETE SET NULL;

-- @phase comment
COMMENT ON TABLE public.inventory_stock_movements IS '库存流水（issue #16；按月分区，审计 DB-004）：每次真源变动一行';

COMMENT ON COLUMN public.inventory_stock_movements.product_id IS '商品 id；可空（迁移 649）—— 无归属货行（台账页直接建的行）的流水没有商品指向，归属看 stock_id';

COMMENT ON COLUMN public.inventory_stock_movements.variant_id IS '变体 id；可空（迁移 649）—— 同上，仅作线索列，真正的归属在 product_variants.warehouse_skus';

COMMENT ON COLUMN public.inventory_stock_movements.unit_cost IS '这次变动时刻的单位成本留痕（元）：出库 = 扣减时该库存行的当前成本；入库 = 本次显式成本（采购 / 生产单价），无显式成本时记库存行当前成本；NULL = 当时尚未核算';

COMMENT ON COLUMN public.inventory_stock_movements.stock_id IS '这条变动动的是哪一条库存货行；货行被删时置空，历史仍可读';

COMMENT ON COLUMN public.inventory_stock_movements.stock_name IS '流水写入时刻的货品名称快照（货行主档 name，迁移 642）';

COMMENT ON COLUMN public.inventory_stock_movements.stock_image_url IS '流水写入时刻的货品图片快照（货行主档 image_url，迁移 642）；空串 = 没图';
