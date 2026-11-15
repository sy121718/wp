-- table: ai_session（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_session_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_session (
    id bigint NOT NULL,
    session_key character varying(120) NOT NULL,
    title character varying(200) DEFAULT ''::character varying NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    next_seq bigint DEFAULT 1 NOT NULL,
    head_seq bigint DEFAULT 0 NOT NULL,
    compact_count integer DEFAULT 0 NOT NULL,
    context_tokens bigint DEFAULT 0 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER SEQUENCE public.ai_session_id_seq OWNED BY public.ai_session.id;

ALTER TABLE ONLY public.ai_session ALTER COLUMN id SET DEFAULT nextval('public.ai_session_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_session
    ADD CONSTRAINT ai_session_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.ai_session
    ADD CONSTRAINT uk_ai_session_key UNIQUE (session_key);

-- @phase idx
CREATE INDEX idx_ai_session_status_time ON public.ai_session USING btree (status, update_time DESC);

-- @phase comment
COMMENT ON TABLE public.ai_session IS 'AI 会话：持有序号分配、当前模型与压缩计数；内容是 ai_event 的投影';

COMMENT ON COLUMN public.ai_session.session_key IS '会话标识（唯一）：客户端给什么就存什么（不 hash），同一 key 再来即续写同一会话';

COMMENT ON COLUMN public.ai_session.title IS '会话标题（取首条用户消息的摘要，仅用于列表展示）';

COMMENT ON COLUMN public.ai_session.provider_key IS '当前供应商标识（对应 ai_provider.provider_key；存标识不存外键，供应商删除/改名不影响历史会话）';

COMMENT ON COLUMN public.ai_session.model_id IS '当前模型标识（供应商模型目录里的 id，如 deepseek/deepseek-v4.1-flash）';

COMMENT ON COLUMN public.ai_session.status IS '状态：1 进行中 / 0 归档';

COMMENT ON COLUMN public.ai_session.next_seq IS '下一个待分配的事件序号（同一会话内从 1 起递增，分配必须原子，见 ai_session.next_seq 的原子自增口径）';

COMMENT ON COLUMN public.ai_session.head_seq IS '投影可见范围的末尾序号（= 已分配的最大 seq；仅用于展示与增量拉取，不作为真源）';

COMMENT ON COLUMN public.ai_session.compact_count IS '已执行的压缩次数（每次成功的折叠 +1，用于前端提示与可观测）';

COMMENT ON COLUMN public.ai_session.context_tokens IS '当前投影的估算 token 数（每次 append / 折叠后重算；仅用于触发判断与展示，不参与计费）';

COMMENT ON COLUMN public.ai_session.version IS '乐观锁版本号：会话级读-改-写（改标题 / 切模型 / 归档）必须带 version 条件';

COMMENT ON COLUMN public.ai_session.create_by IS '创建人 ID（与 sys_* 家族同口径）';

COMMENT ON COLUMN public.ai_session.update_by IS '最后修改人 ID';

-- @phase data
SELECT pg_catalog.setval('public.ai_session_id_seq', 1, false);
