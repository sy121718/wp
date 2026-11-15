-- table: ai_access_token（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.ai_access_token_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.ai_access_token (
    id bigint NOT NULL,
    name character varying(80) DEFAULT ''::character varying NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    token_prefix character varying(16) DEFAULT ''::character varying NOT NULL,
    token_hash character varying(64) DEFAULT ''::character varying NOT NULL,
    scopes jsonb DEFAULT '[]'::jsonb NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    expires_at timestamp with time zone,
    last_used_time timestamp with time zone,
    revoked_time timestamp with time zone,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER SEQUENCE public.ai_access_token_id_seq OWNED BY public.ai_access_token.id;

ALTER TABLE ONLY public.ai_access_token ALTER COLUMN id SET DEFAULT nextval('public.ai_access_token_id_seq'::regclass);

CREATE UNIQUE INDEX uq_ai_access_token_hash ON public.ai_access_token USING btree (token_hash);

-- @phase constr
ALTER TABLE ONLY public.ai_access_token
    ADD CONSTRAINT ai_access_token_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_ai_access_token_prefix ON public.ai_access_token USING btree (token_prefix);

CREATE INDEX idx_ai_access_token_user ON public.ai_access_token USING btree (user_id, id DESC);

-- @phase comment
COMMENT ON TABLE public.ai_access_token IS '对外访问令牌（PAT）：外部 harness / AI 以某个管理员身份调用本站工具';

COMMENT ON COLUMN public.ai_access_token.name IS '用途备注（人在列表里认它）；同名允许，靠 id 与前缀区分';

COMMENT ON COLUMN public.ai_access_token.user_id IS '归属的后台账号 id：权限判定与审计都顺着它找人；0 = 未记录（历史数据）';

COMMENT ON COLUMN public.ai_access_token.token_prefix IS '明文前若干位，**仅供展示与排错**（不足以还原令牌）';

COMMENT ON COLUMN public.ai_access_token.token_hash IS '明文的 SHA-256；明文不落库，忘了只能重新生成';

COMMENT ON COLUMN public.ai_access_token.scopes IS '权限点子集（JSON 数组，如 ["order:list"]）；实际可用 = 本集合 ∩ 归属账号仍拥有的权限';

COMMENT ON COLUMN public.ai_access_token.status IS '1 = 启用，0 = 已撤销（撤销不删行，审计要能回答「什么时候撤的」）';

COMMENT ON COLUMN public.ai_access_token.expires_at IS '过期时刻；NULL = 不过期（由使用方按需设置）';

COMMENT ON COLUMN public.ai_access_token.last_used_time IS '最近一次成功使用时刻；NULL = 从未使用（列表里用来识别「发了没用」的令牌）';

COMMENT ON COLUMN public.ai_access_token.revoked_time IS '撤销时刻；与 status 一起写';

-- @phase data
SELECT pg_catalog.setval('public.ai_access_token_id_seq', 1, false);
