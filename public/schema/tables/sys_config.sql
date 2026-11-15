-- table: sys_config（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_config_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_config (
    id bigint NOT NULL,
    group_key character varying(50) NOT NULL,
    group_name character varying(100) NOT NULL,
    config_data jsonb NOT NULL,
    remark character varying(500),
    status smallint DEFAULT 1 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

ALTER SEQUENCE public.sys_config_id_seq OWNED BY public.sys_config.id;

ALTER TABLE ONLY public.sys_config ALTER COLUMN id SET DEFAULT nextval('public.sys_config_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_config
    ADD CONSTRAINT sys_config_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.sys_config
    ADD CONSTRAINT uk_sys_config_group_key UNIQUE (group_key);

-- @phase idx
CREATE INDEX idx_sys_config_status ON public.sys_config USING btree (status);

-- @phase comment
COMMENT ON TABLE public.sys_config IS '系统配置表：按 group_key 分组存储 JSON 配置（全局默认值；工程级覆盖在 projects.settings）';

COMMENT ON COLUMN public.sys_config.group_key IS '配置分组键名（唯一）：分组粒度 = 后台一次保存的单元';

COMMENT ON COLUMN public.sys_config.group_name IS '配置分组显示名（后台页面展示用）';

COMMENT ON COLUMN public.sys_config.config_data IS '整组配置 JSON；组内键由代码白名单约束，解析失败一律回退默认值';

COMMENT ON COLUMN public.sys_config.remark IS '备注说明';

COMMENT ON COLUMN public.sys_config.status IS '状态：1 启用 / 0 禁用';

COMMENT ON COLUMN public.sys_config.version IS '乐观锁版本号：整组读-改-写必须带 version 条件，防丢更新';

COMMENT ON COLUMN public.sys_config.create_by IS '创建人 ID（与 sys_* 家族同口径）';

COMMENT ON COLUMN public.sys_config.update_by IS '最后修改人 ID';

-- @phase data
INSERT INTO public.sys_config VALUES
	(1, 'i18n', '国际化', '{"default_lang": "zh-CN", "lang_url_codes": {"en-AU": "en"}, "site_lang_url_mode": "default_plain"}', '全局默认语言与站点语言 URL 方案（唯一来源；工程级覆盖在 projects.settings）', 1, 2, 0, '2026-11-15 20:36:55.969992+08', 0, '2026-11-15 20:36:55.972015+08'),
	(2, 'trade', '交易默认值', '{"default_country": "CN", "default_currency": "CNY"}', '全局默认国家 / 货币（兜底；工程级覆盖走 projects.settings）', 1, 1, 0, '2026-11-15 20:36:55.977584+08', 0, '2026-11-15 20:36:55.977584+08'),
	(3, 'ai', 'AI 与外部接入', '{"mcp_enabled": false}', 'mcp_enabled：对外 MCP 接入点（POST /mcp）是否可达。默认关闭，必须显式打开；关闭时端点对任何请求都回 404。', 1, 1, 0, '2026-11-15 20:36:56.063747+08', 0, '2026-11-15 20:36:56.063747+08') ON CONFLICT DO NOTHING;

SELECT pg_catalog.setval('public.sys_config_id_seq', 3, true);
