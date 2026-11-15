-- table: order_shipment_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.order_shipment_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.order_shipment_items (
    id bigint NOT NULL,
    shipment_id uuid NOT NULL,
    order_item_id bigint NOT NULL,
    warehouse_id uuid NOT NULL,
    stock_id uuid,
    quantity integer NOT NULL,
    CONSTRAINT ck_order_shipment_items_qty CHECK ((quantity > 0))
);

ALTER SEQUENCE public.order_shipment_items_id_seq OWNED BY public.order_shipment_items.id;

ALTER TABLE ONLY public.order_shipment_items ALTER COLUMN id SET DEFAULT nextval('public.order_shipment_items_id_seq'::regclass);

CREATE UNIQUE INDEX uq_order_shipment_items_row ON public.order_shipment_items USING btree (shipment_id, order_item_id, warehouse_id);

-- @phase constr
ALTER TABLE ONLY public.order_shipment_items
    ADD CONSTRAINT order_shipment_items_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_shipment_items_item ON public.order_shipment_items USING btree (order_item_id);

-- @phase fk
ALTER TABLE ONLY public.order_shipment_items
    ADD CONSTRAINT order_shipment_items_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES public.order_shipments(id) ON DELETE CASCADE;

-- @phase data
SELECT pg_catalog.setval('public.order_shipment_items_id_seq', 1, false);
