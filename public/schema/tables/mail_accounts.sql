-- table: mail_accounts（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_accounts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_accounts (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    purpose character varying(16) DEFAULT 'transactional'::character varying NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    from_name character varying(100),
    from_email character varying(254) NOT NULL,
    reply_to character varying(254),
    provider character varying(32) DEFAULT 'smtp'::character varying NOT NULL,
    host character varying(255),
    port integer,
    username character varying(255),
    password_cipher text,
    encryption character varying(16) DEFAULT 'starttls'::character varying NOT NULL,
    rate_per_hour integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    last_check_at timestamp with time zone,
    last_check_error text,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.mail_accounts_id_seq OWNED BY public.mail_accounts.id;

ALTER TABLE ONLY public.mail_accounts ALTER COLUMN id SET DEFAULT nextval('public.mail_accounts_id_seq'::regclass);

CREATE UNIQUE INDEX idx_mail_accounts_default ON public.mail_accounts USING btree (purpose) WHERE is_default;

-- @phase constr
ALTER TABLE ONLY public.mail_accounts
    ADD CONSTRAINT mail_accounts_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_accounts_purpose ON public.mail_accounts USING btree (purpose);

-- @phase comment
COMMENT ON COLUMN public.mail_accounts.last_check_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_accounts.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_accounts.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
