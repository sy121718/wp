-- table: mail_contacts（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_contacts (
    id bigint NOT NULL,
    email character varying(254) NOT NULL,
    name character varying(128),
    user_id bigint,
    source character varying(16) DEFAULT 'manual'::character varying NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    subscribed_at timestamp with time zone,
    consent_source character varying(64),
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    attributes jsonb,
    last_activity_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.mail_contacts_id_seq OWNED BY public.mail_contacts.id;

ALTER TABLE ONLY public.mail_contacts ALTER COLUMN id SET DEFAULT nextval('public.mail_contacts_id_seq'::regclass);

CREATE UNIQUE INDEX idx_mail_contacts_email ON public.mail_contacts USING btree (lower((email)::text));

-- @phase constr
ALTER TABLE ONLY public.mail_contacts
    ADD CONSTRAINT mail_contacts_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_contacts_status ON public.mail_contacts USING btree (status);

CREATE INDEX idx_mail_contacts_tags ON public.mail_contacts USING gin (tags);

CREATE INDEX idx_mail_contacts_user_id ON public.mail_contacts USING btree (user_id) WHERE (user_id IS NOT NULL);

-- @phase comment
COMMENT ON COLUMN public.mail_contacts.subscribed_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_contacts.last_activity_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_contacts.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_contacts.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
