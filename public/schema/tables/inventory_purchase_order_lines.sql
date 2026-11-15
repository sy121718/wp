-- table: inventory_purchase_order_lines（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_purchase_order_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    order_id uuid NOT NULL,
    project_id uuid NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    received_quantity integer DEFAULT 0 NOT NULL,
    unit_price numeric(12,2) NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_order_lines_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_order_lines_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_order_lines_price_check CHECK ((unit_price >= (0)::numeric)),
    CONSTRAINT inventory_purchase_order_lines_qty_check CHECK ((quantity > 0)),
    CONSTRAINT inventory_purchase_order_lines_received_check CHECK (((received_quantity >= 0) AND (received_quantity <= quantity)))
);

ALTER TABLE ONLY public.inventory_purchase_order_lines FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_purchase_lines_order_variant ON public.inventory_purchase_order_lines USING btree (order_id, variant_id);

CREATE TRIGGER trg_inventory_purchase_lines_status_sync AFTER INSERT OR DELETE OR UPDATE ON public.inventory_purchase_order_lines FOR EACH ROW EXECUTE FUNCTION public.fn_inventory_purchase_order_status_sync();

ALTER TABLE public.inventory_purchase_order_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_purchase_order_lines USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_purchase_lines_order ON public.inventory_purchase_order_lines USING btree (order_id, sort, create_time);

CREATE INDEX idx_inventory_purchase_lines_variant ON public.inventory_purchase_order_lines USING btree (project_id, variant_id);

-- @phase fk
ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT fk_inventory_purchase_lines_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT fk_inventory_purchase_lines_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.inventory_purchase_orders(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.inventory_purchase_order_lines IS '采购行：SKU × 采购数量 × 采购单价 × 已入库数量（原子递增，恒不超过采购数量）';
