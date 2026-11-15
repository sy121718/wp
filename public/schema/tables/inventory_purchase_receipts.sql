-- table: inventory_purchase_receipts（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_purchase_receipts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    kind text DEFAULT 'purchase'::text NOT NULL,
    order_id uuid,
    source_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    request_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'posted'::text NOT NULL,
    movement_batch_id text DEFAULT ''::text NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_receipts_created_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_receipts_kind_check CHECK ((kind = ANY (ARRAY['purchase'::text, 'production'::text]))),
    CONSTRAINT inventory_purchase_receipts_order_kind_check CHECK (((kind = 'purchase'::text) = (order_id IS NOT NULL))),
    CONSTRAINT inventory_purchase_receipts_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'posted'::text])))
);

ALTER TABLE ONLY public.inventory_purchase_receipts FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_purchase_receipts_project_code ON public.inventory_purchase_receipts USING btree (project_id, upper(code));

CREATE UNIQUE INDEX uq_inventory_purchase_receipts_project_request ON public.inventory_purchase_receipts USING btree (project_id, request_id) WHERE (request_id <> ''::text);

ALTER TABLE public.inventory_purchase_receipts ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_purchase_receipts USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_purchase_receipts_order ON public.inventory_purchase_receipts USING btree (order_id, received_at DESC);

CREATE INDEX idx_inventory_purchase_receipts_source ON public.inventory_purchase_receipts USING btree (source_id, received_at DESC);

-- @phase fk
ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.inventory_purchase_orders(id);

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.inventory_sources(id);

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id);

-- @phase comment
COMMENT ON TABLE public.inventory_purchase_receipts IS '入库单头：purchase 采购收货（必有采购单）/ production 自家工厂生产入库（无采购单，成本手工）';

COMMENT ON COLUMN public.inventory_purchase_receipts.request_id IS '幂等键：工程内唯一，重复提交命中既有入库单，不会重复入库';
