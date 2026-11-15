-- table: ai_call_log（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_call_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_call_log (
    id bigint NOT NULL,
    session_id bigint DEFAULT 0 NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    protocol character varying(50) DEFAULT ''::character varying NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    usage_reported boolean DEFAULT false NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    status character varying(20) DEFAULT ''::character varying NOT NULL,
    error_key character varying(80) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    cached_tokens bigint DEFAULT 0 NOT NULL,
    cached_reported boolean DEFAULT false NOT NULL
);

ALTER SEQUENCE public.ai_call_log_id_seq OWNED BY public.ai_call_log.id;

ALTER TABLE ONLY public.ai_call_log ALTER COLUMN id SET DEFAULT nextval('public.ai_call_log_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_call_log
    ADD CONSTRAINT ai_call_log_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_ai_call_log_session_time ON public.ai_call_log USING btree (session_id, create_time DESC);

CREATE INDEX idx_ai_call_log_time ON public.ai_call_log USING btree (create_time DESC);

CREATE INDEX idx_ai_call_log_user_time ON public.ai_call_log USING btree (user_id, create_time DESC);

-- @phase comment
COMMENT ON TABLE public.ai_call_log IS '大模型调用流水：一次出站调用一行（旁路观测，不参与会话投影与计量）';

COMMENT ON COLUMN public.ai_call_log.session_id IS '这次调用属于哪条会话；0 = 未记录（对外接口没有会话概念）';

COMMENT ON COLUMN public.ai_call_log.user_id IS '发起这次调用的后台账号 id；0 = 未记录';

COMMENT ON COLUMN public.ai_call_log.provider_key IS '实际出站用的供应商标识（trim 后的值，不是请求原文）';

COMMENT ON COLUMN public.ai_call_log.model_id IS '实际出站用的模型标识';

COMMENT ON COLUMN public.ai_call_log.protocol IS '实际出站用的协议（取自供应商配置）';

COMMENT ON COLUMN public.ai_call_log.usage_reported IS '上游是否上报了用量；false 时三个 token 列恒为 0（「不知道」而不是「0 消耗」）';

COMMENT ON COLUMN public.ai_call_log.latency_ms IS '从发起到拿到响应的墙钟耗时（毫秒，含 SSRF 校验与响应解析）';

COMMENT ON COLUMN public.ai_call_log.status IS 'ok / error（取值见 aienums.CallStatus）';

COMMENT ON COLUMN public.ai_call_log.error_key IS '失败时的 i18n key（与 enums 哨兵同源）；不存底层原文';

COMMENT ON COLUMN public.ai_call_log.cached_tokens IS '输入里命中上游前缀缓存的 token 数（上游上报值，未报为 0）';

COMMENT ON COLUMN public.ai_call_log.cached_reported IS '上游是否报告过缓存命中数（false 时 cached_tokens 的 0 无意义）';

-- @phase data
SELECT pg_catalog.setval('public.ai_call_log_id_seq', 1, false);
