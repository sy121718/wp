-- table: mail_automation_runs（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_automation_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_automation_runs (
    id bigint NOT NULL,
    automation_id bigint NOT NULL,
    automation_version integer DEFAULT 1 NOT NULL,
    contact_id bigint NOT NULL,
    status character varying(16) DEFAULT 'running'::character varying NOT NULL,
    current_node character varying(64),
    next_run_at timestamp with time zone,
    error_message text,
    trigger_event character varying(32),
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.mail_automation_runs_id_seq OWNED BY public.mail_automation_runs.id;

ALTER TABLE ONLY public.mail_automation_runs ALTER COLUMN id SET DEFAULT nextval('public.mail_automation_runs_id_seq'::regclass);

CREATE UNIQUE INDEX idx_mail_automation_runs_active ON public.mail_automation_runs USING btree (automation_id, contact_id) WHERE ((status)::text = ANY ((ARRAY['running'::character varying, 'waiting'::character varying])::text[]));

-- @phase constr
ALTER TABLE ONLY public.mail_automation_runs
    ADD CONSTRAINT mail_automation_runs_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_mail_automation_runs_automation_contact ON public.mail_automation_runs USING btree (automation_id, contact_id);

CREATE INDEX idx_mail_automation_runs_contact ON public.mail_automation_runs USING btree (contact_id);

CREATE INDEX idx_mail_automation_runs_due ON public.mail_automation_runs USING btree (next_run_at) WHERE ((status)::text = 'waiting'::text);

-- @phase comment
COMMENT ON COLUMN public.mail_automation_runs.next_run_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_automation_runs.started_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_automation_runs.finished_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_automation_runs.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_automation_runs.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
