-- table: ai_tool_call_log（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_tool_call_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_tool_call_log (
    id bigint NOT NULL,
    session_id bigint DEFAULT 0 NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    tool_name character varying(80) DEFAULT ''::character varying NOT NULL,
    arguments_summary character varying(500) DEFAULT ''::character varying NOT NULL,
    result_summary character varying(500) DEFAULT ''::character varying NOT NULL,
    result_len bigint DEFAULT 0 NOT NULL,
    truncated boolean DEFAULT false NOT NULL,
    status character varying(20) DEFAULT ''::character varying NOT NULL,
    error_key character varying(80) DEFAULT ''::character varying NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER SEQUENCE public.ai_tool_call_log_id_seq OWNED BY public.ai_tool_call_log.id;

ALTER TABLE ONLY public.ai_tool_call_log ALTER COLUMN id SET DEFAULT nextval('public.ai_tool_call_log_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_tool_call_log
    ADD CONSTRAINT ai_tool_call_log_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_ai_tool_call_log_session ON public.ai_tool_call_log USING btree (session_id, id DESC);

CREATE INDEX idx_ai_tool_call_log_time ON public.ai_tool_call_log USING btree (create_time DESC);

CREATE INDEX idx_ai_tool_call_log_tool_status ON public.ai_tool_call_log USING btree (tool_name, status);

CREATE INDEX idx_ai_tool_call_log_user ON public.ai_tool_call_log USING btree (user_id, id DESC);

-- @phase comment
COMMENT ON TABLE public.ai_tool_call_log IS '模型工具调用流水：一次工具调用一行（旁路观测，不参与会话投影与计量）';

COMMENT ON COLUMN public.ai_tool_call_log.session_id IS '这次调用属于哪条会话；0 = 未记录';

COMMENT ON COLUMN public.ai_tool_call_log.user_id IS '发起这次调用的后台账号 id（服务端从登录态取，不采信请求参数）；0 = 未记录';

COMMENT ON COLUMN public.ai_tool_call_log.tool_name IS '被调用的工具名（注册表里的名字）';

COMMENT ON COLUMN public.ai_tool_call_log.arguments_summary IS '模型给的参数**摘要**（超长截断）；原文在 ai_event 的工具事件里';

COMMENT ON COLUMN public.ai_tool_call_log.result_summary IS '工具结果**摘要**（超长截断）；全文在 ai_event 的工具事件里';

COMMENT ON COLUMN public.ai_tool_call_log.result_len IS '结果原文长度（字符数）；summary 被截断时这里是全量长度';

COMMENT ON COLUMN public.ai_tool_call_log.truncated IS '交给模型的结果是否被剪枝过（模型看到的是剪枝后的版本）';

COMMENT ON COLUMN public.ai_tool_call_log.status IS 'ok / args_error / forbidden / failed（取值见 aienums.ToolCallStatus）';

COMMENT ON COLUMN public.ai_tool_call_log.error_key IS '失败时的 i18n key（与 enums 哨兵同源）；不存底层原文';

COMMENT ON COLUMN public.ai_tool_call_log.latency_ms IS '工具执行耗时（毫秒，含权限判定与参数校验）';

-- @phase data
SELECT pg_catalog.setval('public.ai_tool_call_log_id_seq', 1, false);
