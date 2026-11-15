-- table: webhook_endpoints（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.webhook_endpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.webhook_endpoints (
    id bigint NOT NULL,
    event_type character varying(128) NOT NULL,
    target_url character varying(1024) NOT NULL,
    secret_cipher character varying(512) NOT NULL,
    description character varying(512) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone CONSTRAINT webhook_endpoints_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT webhook_endpoints_updated_at_not_null NOT NULL
);

ALTER SEQUENCE public.webhook_endpoints_id_seq OWNED BY public.webhook_endpoints.id;

ALTER TABLE ONLY public.webhook_endpoints ALTER COLUMN id SET DEFAULT nextval('public.webhook_endpoints_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_webhook_endpoints_event_type ON public.webhook_endpoints USING btree (event_type);

CREATE INDEX idx_webhook_endpoints_status ON public.webhook_endpoints USING btree (status);

-- @phase data
SELECT pg_catalog.setval('public.webhook_endpoints_id_seq', 1, false);
