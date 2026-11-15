-- table: admin_side_effect_tasks（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.admin_side_effect_tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.admin_side_effect_tasks (
    id bigint NOT NULL,
    kind text NOT NULL,
    payload text NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    claimed_time timestamp with time zone,
    processed_time timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL
);

ALTER SEQUENCE public.admin_side_effect_tasks_id_seq OWNED BY public.admin_side_effect_tasks.id;

ALTER TABLE ONLY public.admin_side_effect_tasks ALTER COLUMN id SET DEFAULT nextval('public.admin_side_effect_tasks_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.admin_side_effect_tasks
    ADD CONSTRAINT admin_side_effect_tasks_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_admin_side_effect_tasks_pending ON public.admin_side_effect_tasks USING btree (id) WHERE (processed_time IS NULL);

-- @phase comment
COMMENT ON TABLE public.admin_side_effect_tasks IS 'admin 提交后副作用的持久重放队列（策略重载 / 会话吊销）';

COMMENT ON COLUMN public.admin_side_effect_tasks.kind IS '副作用种类：policy_reload / revoke_session';

COMMENT ON COLUMN public.admin_side_effect_tasks.payload IS '执行该副作用需要的业务事实（JSON）';

-- @phase data
SELECT pg_catalog.setval('public.admin_side_effect_tasks_id_seq', 1, false);
