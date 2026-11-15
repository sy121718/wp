-- table: webhook_deliveries（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.webhook_deliveries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.webhook_deliveries (
    id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    event_type character varying(128) NOT NULL,
    payload text NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    response_status integer,
    last_error character varying(1024) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone CONSTRAINT webhook_deliveries_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT webhook_deliveries_updated_at_not_null NOT NULL
);

ALTER SEQUENCE public.webhook_deliveries_id_seq OWNED BY public.webhook_deliveries.id;

ALTER TABLE ONLY public.webhook_deliveries ALTER COLUMN id SET DEFAULT nextval('public.webhook_deliveries_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_webhook_deliveries_event_type ON public.webhook_deliveries USING btree (event_type);

CREATE INDEX idx_webhook_deliveries_status ON public.webhook_deliveries USING btree (status);

-- @phase fk
ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.webhook_endpoints(id) ON DELETE CASCADE;
