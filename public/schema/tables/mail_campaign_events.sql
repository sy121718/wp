-- table: mail_campaign_events（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_campaign_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_campaign_events (
    id bigint NOT NULL,
    campaign_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    event_type character varying(16) NOT NULL,
    url character varying(1000),
    ip character varying(50),
    user_agent character varying(255),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER SEQUENCE public.mail_campaign_events_id_seq OWNED BY public.mail_campaign_events.id;

ALTER TABLE ONLY public.mail_campaign_events ALTER COLUMN id SET DEFAULT nextval('public.mail_campaign_events_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.mail_campaign_events
    ADD CONSTRAINT mail_campaign_events_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_campaign_events_campaign ON public.mail_campaign_events USING btree (campaign_id, event_type);

CREATE INDEX idx_mail_campaign_events_contact ON public.mail_campaign_events USING btree (contact_id);

CREATE INDEX idx_mail_campaign_events_time ON public.mail_campaign_events USING btree (campaign_id, create_time DESC);

-- @phase comment
COMMENT ON COLUMN public.mail_campaign_events.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
