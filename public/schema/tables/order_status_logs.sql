-- table: order_status_logs（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.order_status_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.order_status_logs (
    id bigint NOT NULL,
    order_id bigint NOT NULL,
    from_status character varying(20) DEFAULT ''::character varying NOT NULL,
    to_status character varying(20) DEFAULT ''::character varying NOT NULL,
    operator_type character varying(20) DEFAULT 'system'::character varying NOT NULL,
    operator_id bigint DEFAULT 0 NOT NULL,
    operator_name character varying(60) DEFAULT ''::character varying NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER SEQUENCE public.order_status_logs_id_seq OWNED BY public.order_status_logs.id;

ALTER TABLE ONLY public.order_status_logs ALTER COLUMN id SET DEFAULT nextval('public.order_status_logs_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.order_status_logs
    ADD CONSTRAINT order_status_logs_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_order_status_logs_order ON public.order_status_logs USING btree (order_id, id);

-- @phase comment
COMMENT ON COLUMN public.order_status_logs.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.order_status_logs_id_seq', 1, false);
