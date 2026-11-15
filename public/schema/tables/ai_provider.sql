-- table: ai_provider（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_provider_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_provider (
    id bigint NOT NULL,
    provider_key character varying(50) NOT NULL,
    display_name character varying(100) NOT NULL,
    base_url character varying(500),
    protocol character varying(50) DEFAULT 'openai_chat_completions'::character varying NOT NULL,
    api_key_cipher text,
    status smallint DEFAULT 1 NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    config_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER SEQUENCE public.ai_provider_id_seq OWNED BY public.ai_provider.id;

ALTER TABLE ONLY public.ai_provider ALTER COLUMN id SET DEFAULT nextval('public.ai_provider_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.ai_provider
    ADD CONSTRAINT ai_provider_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.ai_provider
    ADD CONSTRAINT uk_ai_provider_provider_key UNIQUE (provider_key);

-- @phase idx
CREATE INDEX idx_ai_provider_sort ON public.ai_provider USING btree (sort);

CREATE INDEX idx_ai_provider_status ON public.ai_provider USING btree (status);

-- @phase comment
COMMENT ON TABLE public.ai_provider IS 'AI 供应商表：一家供应商一行，模型目录放 config_data.models（JSONB 整组替换）';

COMMENT ON COLUMN public.ai_provider.provider_key IS '供应商标识键（唯一，小写）：代码内置默认模型清单按它索引，如 openai / deepseek';

COMMENT ON COLUMN public.ai_provider.display_name IS '后台展示名（可改；改它不影响 provider_key 与内置清单的对应）';

COMMENT ON COLUMN public.ai_provider.base_url IS 'API 地址（留空 = 用代码内置的提供商默认地址）；拼接 {base_url}/models 时按 OpenAI 兼容列表接口口径';

COMMENT ON COLUMN public.ai_provider.protocol IS 'API 协议：openai_chat_completions / openai_responses / anthropic_messages / gemini_generate_content';

COMMENT ON COLUMN public.ai_provider.api_key_cipher IS 'API 密钥密文（pkg/crypto AES-256-GCM，base64(nonce||ciphertext||tag)）；任何接口只回是否已配置，不明文回传';

COMMENT ON COLUMN public.ai_provider.status IS '状态：1 启用 / 0 停用';

COMMENT ON COLUMN public.ai_provider.sort IS '列表排序（升序），同值按 id 升序';

COMMENT ON COLUMN public.ai_provider.config_data IS '补充配置 JSON：只认 models（模型目录数组），其余键写回时原样保留；解析失败一律回退默认值';

COMMENT ON COLUMN public.ai_provider.version IS '乐观锁版本号：整组读-改-写必须带 version 条件，防丢更新';

COMMENT ON COLUMN public.ai_provider.create_by IS '创建人 ID（与 sys_* 家族同口径）';

COMMENT ON COLUMN public.ai_provider.update_by IS '最后修改人 ID';

-- @phase data
SELECT pg_catalog.setval('public.ai_provider_id_seq', 1, false);
