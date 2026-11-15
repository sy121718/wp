-- table: order_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.order_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.order_items (
    id bigint NOT NULL,
    order_id bigint NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name character varying(200) DEFAULT ''::character varying NOT NULL,
    variant_label character varying(200) DEFAULT ''::character varying NOT NULL,
    sku character varying(80) DEFAULT ''::character varying NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    quantity integer DEFAULT 0 NOT NULL,
    line_subtotal bigint DEFAULT 0 NOT NULL,
    line_discount bigint DEFAULT 0 NOT NULL,
    line_tax bigint DEFAULT 0 NOT NULL,
    line_total bigint DEFAULT 0 NOT NULL,
    cost_price bigint,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    warehouse_id uuid
);

ALTER SEQUENCE public.order_items_id_seq OWNED BY public.order_items.id;

ALTER TABLE ONLY public.order_items ALTER COLUMN id SET DEFAULT nextval('public.order_items_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.order_items
    ADD CONSTRAINT order_items_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_items_order ON public.order_items USING btree (order_id);

CREATE INDEX idx_order_items_variant ON public.order_items USING btree (variant_id);

CREATE INDEX idx_order_items_warehouse ON public.order_items USING btree (warehouse_id) WHERE (warehouse_id IS NOT NULL);

-- @phase comment
COMMENT ON COLUMN public.order_items.cost_price IS '下单时刻的行成本快照（分，来自该行归属仓的当前成本）；NULL = 下单时该 (仓库, SKU) 尚未核算（绝不用 0 冒充）';

COMMENT ON COLUMN public.order_items.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.order_items.warehouse_id IS '发货仓（下单时解析并固化；NULL = 存量行，发货时解析后回写）';

-- @phase data
SELECT pg_catalog.setval('public.order_items_id_seq', 1, false);
