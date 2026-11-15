-- table: mail_campaigns（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_campaigns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_campaigns (
    id bigint NOT NULL,
    name character varying(150) NOT NULL,
    account_id bigint NOT NULL,
    template_id bigint NOT NULL,
    target_tags text[] DEFAULT '{}'::text[] NOT NULL,
    subject character varying(255) NOT NULL,
    variables jsonb,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    scheduled_at timestamp with time zone,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    total_count integer DEFAULT 0 NOT NULL,
    sent_count integer DEFAULT 0 NOT NULL,
    failed_count integer DEFAULT 0 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    open_count integer DEFAULT 0 NOT NULL,
    click_count integer DEFAULT 0 NOT NULL
);

ALTER SEQUENCE public.mail_campaigns_id_seq OWNED BY public.mail_campaigns.id;

ALTER TABLE ONLY public.mail_campaigns ALTER COLUMN id SET DEFAULT nextval('public.mail_campaigns_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.mail_campaigns
    ADD CONSTRAINT mail_campaigns_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_campaigns_status ON public.mail_campaigns USING btree (status);

-- @phase comment
COMMENT ON COLUMN public.mail_campaigns.scheduled_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_campaigns.started_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_campaigns.finished_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_campaigns.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_campaigns.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
