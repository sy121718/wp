-- table: mail_automation_node_logs（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_automation_node_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_automation_node_logs (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    node_key character varying(64) NOT NULL,
    node_type character varying(32) NOT NULL,
    status character varying(16) NOT NULL,
    detail text,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER SEQUENCE public.mail_automation_node_logs_id_seq OWNED BY public.mail_automation_node_logs.id;

ALTER TABLE ONLY public.mail_automation_node_logs ALTER COLUMN id SET DEFAULT nextval('public.mail_automation_node_logs_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.mail_automation_node_logs
    ADD CONSTRAINT mail_automation_node_logs_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_automation_node_logs_run ON public.mail_automation_node_logs USING btree (run_id, id);

-- @phase comment
COMMENT ON COLUMN public.mail_automation_node_logs.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
