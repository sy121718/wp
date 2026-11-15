-- table: ai_event（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_event (
    id bigint NOT NULL,
    session_id bigint NOT NULL,
    seq bigint NOT NULL,
    kind character varying(30) NOT NULL,
    surface_op character varying(20) DEFAULT 'append'::character varying NOT NULL,
    replace_from_seq bigint DEFAULT 0 NOT NULL,
    replace_to_seq bigint DEFAULT 0 NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    content_tokens bigint DEFAULT 0 NOT NULL,
    meta jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL
);

ALTER SEQUENCE public.ai_event_id_seq OWNED BY public.ai_event.id;

ALTER TABLE ONLY public.ai_event ALTER COLUMN id SET DEFAULT nextval('public.ai_event_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_event
    ADD CONSTRAINT ai_event_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.ai_event
    ADD CONSTRAINT uk_ai_event_session_seq UNIQUE (session_id, seq);

-- @phase idx
CREATE INDEX idx_ai_event_session_kind ON public.ai_event USING btree (session_id, kind);

CREATE INDEX idx_ai_event_session_model ON public.ai_event USING btree (session_id, provider_key, model_id);

CREATE INDEX idx_ai_event_user_time ON public.ai_event USING btree (user_id, create_time DESC);

-- @phase comment
COMMENT ON TABLE public.ai_event IS 'AI 会话事件日志（append-only 真源）：压缩只追加折叠指令，原文永不删除';

COMMENT ON COLUMN public.ai_event.session_id IS '所属会话 ID';

COMMENT ON COLUMN public.ai_event.seq IS '会话内序号（从 1 起、无洞；唯一约束兼作并发插入的互斥）';

COMMENT ON COLUMN public.ai_event.kind IS '事件类别：user / assistant / tool / compact_start / compact_summary / compact_end / note';

COMMENT ON COLUMN public.ai_event.surface_op IS '可见性操作：append 进投影；replace 把 [replace_from_seq,replace_to_seq] 段替换成本条';

COMMENT ON COLUMN public.ai_event.replace_from_seq IS 'surface_op=replace 时的起始序号（含），否则为 0';

COMMENT ON COLUMN public.ai_event.replace_to_seq IS 'surface_op=replace 时的结束序号（含），否则为 0';

COMMENT ON COLUMN public.ai_event.content IS '事件正文（原文；压缩摘要也写在这里，原文不删）';

COMMENT ON COLUMN public.ai_event.content_tokens IS '本条正文的估算 token 数（用于计量与折叠净收益计算）';

COMMENT ON COLUMN public.ai_event.meta IS '附加信息 JSON：工具名/参数摘要/模型/耗时/错误码/折叠前后 token 等，键由代码白名单约束';

COMMENT ON COLUMN public.ai_event.provider_key IS '产生本条事件的供应商标识；空串 = 未记录（529 之前写入的历史事件）';

COMMENT ON COLUMN public.ai_event.model_id IS '产生本条事件的模型标识；空串 = 未记录（529 之前写入的历史事件）';

COMMENT ON COLUMN public.ai_event.user_id IS '写入这条事件的后台账号 id；0 = 未记录（535 之前写入的历史事件）';
