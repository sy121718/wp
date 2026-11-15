-- table: inventory_purchase_orders（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_purchase_orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    source_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    status text DEFAULT 'ordered'::text NOT NULL,
    ordered_at timestamp with time zone DEFAULT now() NOT NULL,
    expected_at timestamp with time zone,
    remark text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_orders_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_orders_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_orders_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'ordered'::text, 'partial'::text, 'received'::text, 'exception'::text, 'finished'::text, 'cancelled'::text])))
);

ALTER TABLE ONLY public.inventory_purchase_orders FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_purchase_orders_project_code ON public.inventory_purchase_orders USING btree (project_id, upper(code));

CREATE TRIGGER trg_inventory_purchase_orders_status_guard BEFORE UPDATE OF status ON public.inventory_purchase_orders FOR EACH ROW EXECUTE FUNCTION public.fn_inventory_purchase_order_status_guard();

ALTER TABLE public.inventory_purchase_orders ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_purchase_orders USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_purchase_orders_project_status ON public.inventory_purchase_orders USING btree (project_id, status, create_time DESC);

CREATE INDEX idx_inventory_purchase_orders_source ON public.inventory_purchase_orders USING btree (source_id);

-- @phase fk
ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.inventory_sources(id);

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id);

-- @phase comment
COMMENT ON TABLE public.inventory_purchase_orders IS '采购单头（来源 = inventory_sources 货源，issue #18）';

COMMENT ON COLUMN public.inventory_purchase_orders.status IS 'draft 草稿（未下单，不可收货）/ ordered 已下单待收货 / partial 部分到货 / received 已到货 / exception 异常（暂停收货，可解除）/ finished 提前关单 / cancelled 取消';
