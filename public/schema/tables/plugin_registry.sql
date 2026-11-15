-- table: plugin_registry（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.plugin_registry (
    plugin_id text NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    schema_version integer DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    manifest jsonb NOT NULL,
    storage_path text DEFAULT ''::text NOT NULL,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT plugin_registry_updated_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.plugin_registry
    ADD CONSTRAINT plugin_registry_pkey PRIMARY KEY (plugin_id);

-- @phase comment
COMMENT ON TABLE public.plugin_registry IS '插件注册表：安装/版本/启停记账（docs/06-plugin-system.md）';
