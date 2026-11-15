-- table: user_profiles（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.user_profiles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.user_profiles (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    first_name character varying(60),
    last_name character varying(60),
    gender smallint DEFAULT 0 NOT NULL,
    birthday date,
    bio text,
    website character varying(255),
    locale character varying(16),
    timezone character varying(64),
    country character varying(64),
    province character varying(64),
    city character varying(64),
    address character varying(255),
    postcode character varying(20),
    phone character varying(20),
    company character varying(100),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.user_profiles_id_seq OWNED BY public.user_profiles.id;

ALTER TABLE ONLY public.user_profiles ALTER COLUMN id SET DEFAULT nextval('public.user_profiles_id_seq'::regclass);

CREATE UNIQUE INDEX idx_user_profiles_user_id ON public.user_profiles USING btree (user_id);

-- @phase constr
ALTER TABLE ONLY public.user_profiles
    ADD CONSTRAINT user_profiles_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON COLUMN public.user_profiles.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.user_profiles.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
