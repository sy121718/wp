-- table: orders（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.orders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.orders (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    order_no character varying(40) NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    user_id bigint,
    customer_email character varying(120) DEFAULT ''::character varying NOT NULL,
    customer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    customer_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    currency character varying(8) DEFAULT 'CNY'::character varying NOT NULL,
    subtotal bigint DEFAULT 0 NOT NULL,
    discount_total bigint DEFAULT 0 NOT NULL,
    shipping_total bigint DEFAULT 0 NOT NULL,
    tax_total bigint DEFAULT 0 NOT NULL,
    total bigint DEFAULT 0 NOT NULL,
    ship_name character varying(60) DEFAULT ''::character varying NOT NULL,
    ship_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_province character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_city character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_district character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_address character varying(255) DEFAULT ''::character varying NOT NULL,
    ship_zip character varying(20) DEFAULT ''::character varying NOT NULL,
    bill_name character varying(60) DEFAULT ''::character varying NOT NULL,
    bill_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_province character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_city character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_district character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_address character varying(255) DEFAULT ''::character varying NOT NULL,
    bill_zip character varying(20) DEFAULT ''::character varying NOT NULL,
    payment_method character varying(40) DEFAULT ''::character varying NOT NULL,
    payment_method_title character varying(60) DEFAULT ''::character varying NOT NULL,
    transaction_id character varying(120) DEFAULT ''::character varying NOT NULL,
    paid_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_via character varying(20) DEFAULT 'checkout'::character varying NOT NULL,
    ip_address character varying(50) DEFAULT ''::character varying NOT NULL,
    user_agent character varying(255) DEFAULT ''::character varying NOT NULL,
    attribution jsonb DEFAULT '{}'::jsonb NOT NULL,
    admin_note character varying(500) DEFAULT ''::character varying NOT NULL,
    request_id character varying(64) DEFAULT ''::character varying NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    cancel_reason character varying(255) DEFAULT ''::character varying NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    membership_discount_total bigint DEFAULT 0 NOT NULL,
    ship_country character varying(2) DEFAULT ''::character varying NOT NULL,
    bill_country character varying(2) DEFAULT ''::character varying NOT NULL,
    carrier_id uuid,
    carrier_name text DEFAULT ''::text NOT NULL,
    tracking_no text DEFAULT ''::text NOT NULL,
    shipped_at timestamp with time zone,
    fx_rate numeric(18,8),
    fx_base_code text DEFAULT ''::text NOT NULL,
    fulfillment_status text DEFAULT 'pending'::text NOT NULL,
    CONSTRAINT ck_orders_fulfillment_status CHECK ((fulfillment_status = ANY (ARRAY['pending'::text, 'paid'::text, 'shipped'::text, 'partial'::text, 'received'::text, 'exception'::text, 'cancelled'::text, 'reship'::text, 'returned'::text, 'return_done'::text, 'refunded'::text, 'failed'::text]))),
    CONSTRAINT orders_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'shipped'::character varying, 'completed'::character varying, 'cancelled'::character varying, 'refunded'::character varying])::text[])))
);

ALTER TABLE ONLY public.orders FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.orders_id_seq OWNED BY public.orders.id;

ALTER TABLE ONLY public.orders ALTER COLUMN id SET DEFAULT nextval('public.orders_id_seq'::regclass);

CREATE UNIQUE INDEX uk_orders_request_id ON public.orders USING btree (request_id) WHERE ((request_id)::text <> ''::text);

ALTER TABLE public.orders ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.orders USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT uk_orders_no UNIQUE (order_no);

-- @phase idx
CREATE INDEX idx_orders_create_time ON public.orders USING btree (create_time DESC);

CREATE INDEX idx_orders_customer_email ON public.orders USING btree (customer_email);

CREATE INDEX idx_orders_customer_email_trgm ON public.orders USING gin (customer_email ext_shared.gin_trgm_ops);

CREATE INDEX idx_orders_customer_name_trgm ON public.orders USING gin (customer_name ext_shared.gin_trgm_ops);

CREATE INDEX idx_orders_order_no_trgm ON public.orders USING gin (order_no ext_shared.gin_trgm_ops);

CREATE INDEX idx_orders_project_fulfillment ON public.orders USING btree (project_id, fulfillment_status);

CREATE INDEX idx_orders_project_status ON public.orders USING btree (project_id, status);

CREATE INDEX idx_orders_project_status_time ON public.orders USING btree (project_id, status, create_time DESC);

CREATE INDEX idx_orders_transaction ON public.orders USING btree (transaction_id) WHERE ((transaction_id)::text <> ''::text);

CREATE INDEX idx_orders_user ON public.orders USING btree (user_id) WHERE (user_id IS NOT NULL);

-- @phase comment
COMMENT ON COLUMN public.orders.paid_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.orders.completed_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.orders.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.orders.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.orders.membership_discount_total IS '会员折扣金额（分，BIZ-3）：与 discount_total（券）相加即总扣减，两列各自独立计账 —— 不改 discount_total 的既有语义（SEC-001：无券时折扣恒为 0）';

COMMENT ON COLUMN public.orders.ship_country IS '收货地址国家/地区代码快照（ISO 3166-1 alpha-2，如 CN）；空 = 未收集（非中国站点 / 存量数据），不随字典变化';

COMMENT ON COLUMN public.orders.bill_country IS '账单地址国家/地区代码快照（ISO 3166-1 alpha-2，如 CN）；空 = 未收集，不随字典变化';

COMMENT ON COLUMN public.orders.carrier_name IS '最近一次发货的承运商名称快照（历史发货看 order_shipments）';

COMMENT ON COLUMN public.orders.tracking_no IS '最近一次发货的运单号快照（历史发货看 order_shipments）';

COMMENT ON COLUMN public.orders.shipped_at IS '最近一次发货时刻（历史发货看 order_shipments）';

COMMENT ON COLUMN public.orders.fx_rate IS '下单时刻汇率（1 订单币 = fx_rate 基准币）；NULL = 汇率未固化，不推算';

COMMENT ON COLUMN public.orders.fx_base_code IS '汇率快照的基准币种（fx_rate 的分母）；空串 = 未固化';

COMMENT ON COLUMN public.orders.fulfillment_status IS '履约状态（货的语义，独立于 status 的钱的语义；B7 迁移 652）';
