-- table: inventory_transfer_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.inventory_transfer_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.inventory_transfer_items (
    id bigint NOT NULL,
    transfer_id uuid NOT NULL,
    stock_id uuid NOT NULL,
    variant_id uuid,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    CONSTRAINT ck_inventory_transfer_items_qty CHECK ((quantity > 0))
);

ALTER SEQUENCE public.inventory_transfer_items_id_seq OWNED BY public.inventory_transfer_items.id;

ALTER TABLE ONLY public.inventory_transfer_items ALTER COLUMN id SET DEFAULT nextval('public.inventory_transfer_items_id_seq'::regclass);

CREATE UNIQUE INDEX uq_inventory_transfer_items_row ON public.inventory_transfer_items USING btree (transfer_id, stock_id);

-- @phase constr
ALTER TABLE ONLY public.inventory_transfer_items
    ADD CONSTRAINT inventory_transfer_items_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.inventory_transfer_items
    ADD CONSTRAINT inventory_transfer_items_transfer_id_fkey FOREIGN KEY (transfer_id) REFERENCES public.inventory_transfers(id) ON DELETE CASCADE;
