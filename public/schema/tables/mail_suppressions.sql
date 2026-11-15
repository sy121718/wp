-- table: mail_suppressions（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_suppressions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_suppressions (
    id bigint NOT NULL,
    email character varying(254) NOT NULL,
    reason character varying(16) NOT NULL,
    source character varying(64),
    note character varying(255),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER SEQUENCE public.mail_suppressions_id_seq OWNED BY public.mail_suppressions.id;

ALTER TABLE ONLY public.mail_suppressions ALTER COLUMN id SET DEFAULT nextval('public.mail_suppressions_id_seq'::regclass);

CREATE UNIQUE INDEX idx_mail_suppressions_email ON public.mail_suppressions USING btree (lower((email)::text));

-- @phase constr
ALTER TABLE ONLY public.mail_suppressions
    ADD CONSTRAINT mail_suppressions_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON COLUMN public.mail_suppressions.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.mail_suppressions_id_seq', 1, false);
