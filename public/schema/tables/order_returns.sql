-- table: order_returns（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.order_returns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.order_returns (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    order_id bigint NOT NULL,
    order_no character varying(40) DEFAULT ''::character varying NOT NULL,
    return_no character varying(40) NOT NULL,
    status character varying(20) DEFAULT 'requested'::character varying NOT NULL,
    reason character varying(255) DEFAULT ''::character varying NOT NULL,
    refund_amount bigint DEFAULT 0 NOT NULL,
    user_id bigint,
    customer_email character varying(120) DEFAULT ''::character varying NOT NULL,
    customer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    admin_note character varying(500) DEFAULT ''::character varying NOT NULL,
    reviewer_id bigint DEFAULT 0 NOT NULL,
    reviewer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    reviewed_at timestamp with time zone,
    received_at timestamp with time zone,
    refunded_at timestamp with time zone,
    transaction_id character varying(120) DEFAULT ''::character varying NOT NULL,
    request_id character varying(64) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_order_returns_amount CHECK ((refund_amount >= 0)),
    CONSTRAINT ck_order_returns_status CHECK (((status)::text = ANY ((ARRAY['requested'::character varying, 'approved'::character varying, 'received'::character varying, 'completed'::character varying, 'rejected'::character varying, 'cancelled'::character varying])::text[])))
);

ALTER TABLE ONLY public.order_returns FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.order_returns_id_seq OWNED BY public.order_returns.id;

ALTER TABLE ONLY public.order_returns ALTER COLUMN id SET DEFAULT nextval('public.order_returns_id_seq'::regclass);

CREATE UNIQUE INDEX uq_order_returns_no ON public.order_returns USING btree (project_id, return_no);

CREATE UNIQUE INDEX uq_order_returns_request ON public.order_returns USING btree (project_id, request_id) WHERE ((request_id)::text <> ''::text);

ALTER TABLE public.order_returns ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.order_returns USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.order_returns
    ADD CONSTRAINT order_returns_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_returns_order ON public.order_returns USING btree (order_id);

CREATE INDEX idx_order_returns_project_status ON public.order_returns USING btree (project_id, status);

CREATE INDEX idx_order_returns_user ON public.order_returns USING btree (user_id);

-- @phase comment
COMMENT ON COLUMN public.order_returns.reviewed_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.order_returns.received_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.order_returns.refunded_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.order_returns.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.order_returns.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.order_returns_id_seq', 1, false);
