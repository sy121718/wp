-- table: mail_automations（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_automations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_automations (
    id bigint NOT NULL,
    name character varying(150) NOT NULL,
    description character varying(500),
    trigger_type character varying(32) DEFAULT 'manual'::character varying NOT NULL,
    trigger_params jsonb,
    definition jsonb NOT NULL,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    trigger_frequency character varying(16) DEFAULT 'every_time'::character varying NOT NULL,
    exit_on_reply boolean DEFAULT false NOT NULL
);

ALTER SEQUENCE public.mail_automations_id_seq OWNED BY public.mail_automations.id;

ALTER TABLE ONLY public.mail_automations ALTER COLUMN id SET DEFAULT nextval('public.mail_automations_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.mail_automations
    ADD CONSTRAINT mail_automations_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_automations_status ON public.mail_automations USING btree (status);

-- @phase comment
COMMENT ON COLUMN public.mail_automations.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_automations.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.mail_automations_id_seq', 1, false);
