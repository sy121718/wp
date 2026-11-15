-- table: order_return_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.order_return_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.order_return_items (
    id bigint NOT NULL,
    return_id bigint NOT NULL,
    order_item_id bigint NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name character varying(200) DEFAULT ''::character varying NOT NULL,
    variant_label character varying(200) DEFAULT ''::character varying NOT NULL,
    sku character varying(80) DEFAULT ''::character varying NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    quantity integer NOT NULL,
    received_quantity integer DEFAULT 0 NOT NULL,
    refund_amount bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_order_return_items_amount CHECK (((refund_amount >= 0) AND (unit_price >= 0))),
    CONSTRAINT ck_order_return_items_qty CHECK (((quantity > 0) AND (received_quantity >= 0) AND (received_quantity <= quantity)))
);

ALTER SEQUENCE public.order_return_items_id_seq OWNED BY public.order_return_items.id;

ALTER TABLE ONLY public.order_return_items ALTER COLUMN id SET DEFAULT nextval('public.order_return_items_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.order_return_items
    ADD CONSTRAINT order_return_items_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_return_items_order_item ON public.order_return_items USING btree (order_item_id);

CREATE INDEX idx_order_return_items_return ON public.order_return_items USING btree (return_id);

-- @phase comment
COMMENT ON COLUMN public.order_return_items.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
