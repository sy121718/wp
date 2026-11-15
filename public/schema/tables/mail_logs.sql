-- table: mail_logs（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_logs (
    id bigint NOT NULL,
    account_id bigint,
    template_key character varying(64),
    to_email character varying(254) NOT NULL,
    subject character varying(255),
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    provider character varying(32),
    provider_msg_id character varying(191),
    error_kind character varying(16),
    error_message text,
    retry_count integer DEFAULT 0 NOT NULL,
    sent_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    campaign_id bigint,
    contact_id bigint
);

ALTER SEQUENCE public.mail_logs_id_seq OWNED BY public.mail_logs.id;

ALTER TABLE ONLY public.mail_logs ALTER COLUMN id SET DEFAULT nextval('public.mail_logs_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.mail_logs
    ADD CONSTRAINT mail_logs_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_logs_campaign ON public.mail_logs USING btree (campaign_id) WHERE (campaign_id IS NOT NULL);

CREATE INDEX idx_mail_logs_contact ON public.mail_logs USING btree (contact_id) WHERE (contact_id IS NOT NULL);

CREATE INDEX idx_mail_logs_create_time ON public.mail_logs USING btree (create_time DESC);

CREATE INDEX idx_mail_logs_status ON public.mail_logs USING btree (status);

CREATE INDEX idx_mail_logs_to_email ON public.mail_logs USING btree (to_email);

-- @phase comment
COMMENT ON COLUMN public.mail_logs.sent_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_logs.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.mail_logs_id_seq', 1, false);
