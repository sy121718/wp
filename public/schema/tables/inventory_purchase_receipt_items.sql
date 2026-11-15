-- table: inventory_purchase_receipt_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_purchase_receipt_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    receipt_id uuid NOT NULL,
    project_id uuid NOT NULL,
    line_id uuid,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    unit_price numeric(12,2) NOT NULL,
    cost_updated boolean DEFAULT false NOT NULL,
    cost_error text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_receipt_items_created_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_receipt_items_price_check CHECK ((unit_price >= (0)::numeric)),
    CONSTRAINT inventory_purchase_receipt_items_qty_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_purchase_receipt_items FORCE ROW LEVEL SECURITY;

ALTER TABLE public.inventory_purchase_receipt_items ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_purchase_receipt_items USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_purchase_receipt_items_receipt ON public.inventory_purchase_receipt_items USING btree (receipt_id);

CREATE INDEX idx_inventory_purchase_receipt_items_sku ON public.inventory_purchase_receipt_items USING btree (project_id, sku_code, create_time DESC);

CREATE INDEX idx_inventory_purchase_receipt_items_variant ON public.inventory_purchase_receipt_items USING btree (project_id, variant_id, create_time DESC);

-- @phase fk
ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT fk_inventory_receipt_items_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT fk_inventory_receipt_items_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_line_id_fkey FOREIGN KEY (line_id) REFERENCES public.inventory_purchase_order_lines(id);

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES public.inventory_purchase_receipts(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.inventory_purchase_receipt_items IS '入库单行：数量与单价快照 + SKU 成本价回写结果';
