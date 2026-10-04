-- 511 · ai_provider：AI 供应商 / 模型配置表（单表 + JSONB 模型目录）＋ 乐观锁版本号。
--
-- 形态沿用 484_sys_config.sql 的口径（固定列承载不可枚举的字段 + JSONB 承载可变的
-- 一组数据），按 ai 域的需要加了 provider_key（UNIQUE）与 api_key_cipher（密文列）。
--
-- 定位（只放什么）：
--   · 只放 AI 供应商的运行期配置：显示名 / API 地址 / 协议 / 加密后的 API 密钥 /
--     模型目录（config_data.models）；后台「设置 → 模型」页是它唯一的写入口。
--   · **不放明文密钥** —— api_key_cipher 存 pkg/crypto 的 base64(nonce||ciphertext||tag)，
--     任何接口只回「是否已配置」，绝不明文回传（加密密钥由装配期注入的 app.secret 派生）。
--   · **不放**会话与调用日志 —— 会话层不在本批范围。
--
-- 为什么模型目录进 JSONB 而不是独立子表：
--   · 目录的编辑单元是**整组替换**（「恢复默认模型」整目录换、「保存」整目录写回），
--     与 sys_config 的「一组 JSON 一次保存」同形；拆子表会引入「目录行的 id 生命周期」
--     这类与产品语义无关的复杂度，而目录行本身没有跨表引用。
--   · 形状由代码白名单约束（只认 config_data.models，其余键写回时原样保留），
--     解析失败一律回退默认值 —— 与 sys_config.config_data 的既有口径一致。
--
-- 全局归属（明确决策，见 docs/16 的 ADR）：本表**刻意不带 project_id** —— AI 供应商
--   （含密钥）是实例级全局对象，跨 project 共用一份配置。代价是「任一持 ai:manage 的账号
--   改错 base_url 会波及全站」，补偿控制落在写入侧：**改 API 地址即清空密钥、强制重填**
--   （service/ai_provider_crud.go），使被改的地址无法带着旧凭据出站（同时也堵住「自己填
--   地址 + 已存密钥 → fetch 把公司凭据外带」的路径）。本表不受 215 的 RLS 保护，
--   属应用层全局配置。
--
-- 乐观锁按行生效（version）：整组读-改-写必须带 version 条件，否则两个管理员改同一
-- 供应商时后写者静默覆盖前者（AGENTS「写操作的事务与回滚」：读-改-写必须有行锁或原子 SQL）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / 独立 COMMENT ON，
-- 重复执行安全。本迁移**带 TableName**（见 register_ai.go）：表已存在即整条跳过，
-- 条件判据只枚举本批自己的对象（to_regclass('ai_provider')）；将来改结构要另开新迁移。

CREATE TABLE IF NOT EXISTS ai_provider (
    id             BIGSERIAL    PRIMARY KEY,
    provider_key   VARCHAR(50)  NOT NULL,
    display_name   VARCHAR(100) NOT NULL,
    base_url       VARCHAR(500),
    protocol       VARCHAR(50)  NOT NULL DEFAULT 'openai_chat_completions',
    api_key_cipher TEXT,
    status         SMALLINT     NOT NULL DEFAULT 1,
    sort           INT          NOT NULL DEFAULT 0,
    config_data    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    version        BIGINT       NOT NULL DEFAULT 1,
    create_by      BIGINT       NOT NULL DEFAULT 0,
    create_time    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_by      BIGINT       NOT NULL DEFAULT 0,
    update_time    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_ai_provider_provider_key UNIQUE (provider_key)
);

CREATE INDEX IF NOT EXISTS idx_ai_provider_status ON ai_provider (status);
CREATE INDEX IF NOT EXISTS idx_ai_provider_sort ON ai_provider (sort);

COMMENT ON TABLE  ai_provider IS 'AI 供应商表：一家供应商一行，模型目录放 config_data.models（JSONB 整组替换）';
COMMENT ON COLUMN ai_provider.provider_key IS '供应商标识键（唯一，小写）：代码内置默认模型清单按它索引，如 openai / deepseek';
COMMENT ON COLUMN ai_provider.display_name IS '后台展示名（可改；改它不影响 provider_key 与内置清单的对应）';
COMMENT ON COLUMN ai_provider.base_url IS 'API 地址（留空 = 用代码内置的提供商默认地址）；拼接 {base_url}/models 时按 OpenAI 兼容列表接口口径';
COMMENT ON COLUMN ai_provider.protocol IS 'API 协议：openai_chat_completions / openai_responses / anthropic_messages / gemini_generate_content';
COMMENT ON COLUMN ai_provider.api_key_cipher IS 'API 密钥密文（pkg/crypto AES-256-GCM，base64(nonce||ciphertext||tag)）；任何接口只回是否已配置，不明文回传';
COMMENT ON COLUMN ai_provider.status IS '状态：1 启用 / 0 停用';
COMMENT ON COLUMN ai_provider.sort IS '列表排序（升序），同值按 id 升序';
COMMENT ON COLUMN ai_provider.config_data IS '补充配置 JSON：只认 models（模型目录数组），其余键写回时原样保留；解析失败一律回退默认值';
COMMENT ON COLUMN ai_provider.version IS '乐观锁版本号：整组读-改-写必须带 version 条件，防丢更新';
COMMENT ON COLUMN ai_provider.create_by IS '创建人 ID（与 sys_* 家族同口径）';
COMMENT ON COLUMN ai_provider.update_by IS '最后修改人 ID';
