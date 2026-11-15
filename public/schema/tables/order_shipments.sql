-- table: order_shipments（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.order_shipments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    order_id bigint NOT NULL,
    shipment_no text NOT NULL,
    carrier_id uuid,
    carrier_name text DEFAULT ''::text NOT NULL,
    tracking_no text DEFAULT ''::text NOT NULL,
    shipped_at timestamp with time zone DEFAULT now() NOT NULL,
    operator_id bigint DEFAULT 0 NOT NULL,
    operator_name text DEFAULT ''::text NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    request_id text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.order_shipments FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_order_shipments_no ON public.order_shipments USING btree (project_id, shipment_no);

CREATE UNIQUE INDEX uq_order_shipments_request ON public.order_shipments USING btree (project_id, request_id) WHERE (request_id <> ''::text);

ALTER TABLE public.order_shipments ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.order_shipments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.order_shipments
    ADD CONSTRAINT order_shipments_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_shipments_order ON public.order_shipments USING btree (order_id, create_time DESC);
