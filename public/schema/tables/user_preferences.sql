-- table: user_preferences（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.user_preferences_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.user_preferences (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    theme character varying(64),
    locale character varying(16),
    timezone character varying(64),
    page_size smallint DEFAULT 20 NOT NULL,
    email_notify boolean DEFAULT true NOT NULL,
    sms_notify boolean DEFAULT false NOT NULL,
    profile_visibility character varying(16) DEFAULT 'public'::character varying NOT NULL,
    show_online boolean DEFAULT true NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.user_preferences_id_seq OWNED BY public.user_preferences.id;

ALTER TABLE ONLY public.user_preferences ALTER COLUMN id SET DEFAULT nextval('public.user_preferences_id_seq'::regclass);

CREATE UNIQUE INDEX idx_user_preferences_user_id ON public.user_preferences USING btree (user_id);

-- @phase constr
ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON COLUMN public.user_preferences.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.user_preferences.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
