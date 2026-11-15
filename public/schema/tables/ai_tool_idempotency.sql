-- table: ai_tool_idempotency（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_tool_idempotency_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_tool_idempotency (
    id bigint NOT NULL,
    tool_name text NOT NULL,
    idem_key text NOT NULL,
    result_text text DEFAULT ''::text NOT NULL,
    result_data jsonb,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    actor bigint DEFAULT 0 NOT NULL
);

ALTER SEQUENCE public.ai_tool_idempotency_id_seq OWNED BY public.ai_tool_idempotency.id;

ALTER TABLE ONLY public.ai_tool_idempotency ALTER COLUMN id SET DEFAULT nextval('public.ai_tool_idempotency_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_tool_idempotency
    ADD CONSTRAINT ai_tool_idempotency_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.ai_tool_idempotency
    ADD CONSTRAINT uk_ai_tool_idempotency_actor UNIQUE (tool_name, actor, idem_key);

-- @phase comment
COMMENT ON TABLE public.ai_tool_idempotency IS '写工具幂等台账：同一次意图的重试返回上次成功的结果';

COMMENT ON COLUMN public.ai_tool_idempotency.idem_key IS '调用方给的幂等键（同一次意图复用同一个值）';

COMMENT ON COLUMN public.ai_tool_idempotency.actor IS '调用者 id（B02 迁移 678）：幂等键的作用域是 (tool_name, actor, idem_key)。0 = 无主体的系统调用或迁移前的存量行';
