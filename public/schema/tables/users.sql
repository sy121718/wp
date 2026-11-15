-- table: users（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.users (
    id bigint NOT NULL,
    username character varying(60) NOT NULL,
    password character varying(100) DEFAULT ''::character varying NOT NULL,
    email character varying(100) DEFAULT ''::character varying NOT NULL,
    email_verified_at timestamp with time zone,
    status smallint DEFAULT 1 NOT NULL,
    nickname character varying(60),
    display_name character varying(250),
    avatar character varying(255),
    activation_key character varying(64),
    activation_expires_at timestamp with time zone,
    login_failure_count integer DEFAULT 0 NOT NULL,
    locked_until_time timestamp with time zone,
    last_failure_time timestamp with time zone,
    register_ip character varying(50),
    register_location character varying(100),
    last_login_ip character varying(50),
    last_login_location character varying(100),
    registered_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_login_time timestamp with time zone,
    last_active_at timestamp with time zone,
    metadata jsonb,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    deleted_at timestamp with time zone,
    search_text text GENERATED ALWAYS AS (lower((((((((COALESCE(username, ''::character varying))::text || ' '::text) || (COALESCE(email, ''::character varying))::text) || ' '::text) || (COALESCE(nickname, ''::character varying))::text) || ' '::text) || (COALESCE(display_name, ''::character varying))::text))) STORED
);

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

CREATE UNIQUE INDEX idx_users_email ON public.users USING btree (lower((email)::text)) WHERE ((email)::text <> ''::text);

CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (lower((username)::text));

-- @phase constr
ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_users_activation_key ON public.users USING btree (activation_key) WHERE (activation_key IS NOT NULL);

CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);

CREATE INDEX idx_users_search_trgm ON public.users USING gin (search_text ext_shared.gin_trgm_ops);

CREATE INDEX idx_users_status ON public.users USING btree (status);

-- @phase comment
COMMENT ON COLUMN public.users.email_verified_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.activation_expires_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.locked_until_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.last_failure_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.registered_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.last_login_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.last_active_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.users.deleted_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.users_id_seq', 1, false);
