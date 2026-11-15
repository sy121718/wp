-- table: blocks（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.blocks (
    project_id uuid NOT NULL,
    name text NOT NULL,
    kind text DEFAULT 'block'::text NOT NULL,
    document jsonb DEFAULT '{"root": [], "settings": {"layout": {"mode": "full"}}}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT blocks_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT blocks_updated_at_not_null NOT NULL,
    category text DEFAULT 'general'::text NOT NULL,
    reuse_mode text DEFAULT 'global'::text NOT NULL,
    id uuid DEFAULT gen_random_uuid() CONSTRAINT blocks_id_uuid_not_null NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    preset_key text,
    preset_version integer,
    CONSTRAINT blocks_kind_check CHECK ((kind = ANY (ARRAY['block'::text, 'header'::text, 'footer'::text, 'announcement'::text, 'sidebar'::text, 'breadcrumb'::text, 'drawer'::text, 'search'::text, 'cta'::text, 'trust'::text, 'brands'::text, 'contact'::text, 'about'::text, 'banner'::text, 'grid'::text, 'snippet'::text]))),
    CONSTRAINT blocks_reuse_mode_check CHECK ((reuse_mode = ANY (ARRAY['global'::text, 'template'::text]))),
    CONSTRAINT ck_blocks_preset_pair CHECK (((preset_key IS NULL) = (preset_version IS NULL))),
    CONSTRAINT ck_blocks_version_positive CHECK ((version >= 1))
);

ALTER TABLE ONLY public.blocks FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_blocks_project_name ON public.blocks USING btree (project_id, name);

CREATE UNIQUE INDEX uq_blocks_project_name_lower ON public.blocks USING btree (project_id, lower(name));

ALTER TABLE public.blocks ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.blocks USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.blocks
    ADD CONSTRAINT blocks_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_blocks_category ON public.blocks USING btree (project_id, category);

CREATE INDEX idx_blocks_reuse_mode ON public.blocks USING btree (project_id, reuse_mode);

-- @phase fk
ALTER TABLE ONLY public.blocks
    ADD CONSTRAINT blocks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON COLUMN public.blocks.version IS '乐观并发版本（B2 迁移 676）：保存必须带 expectedVersion，不匹配回 409。与文档根的 schemaVersion 是两件事';

COMMENT ON COLUMN public.blocks.preset_key IS '本块由哪个内置预设复制而来（B2 迁移 676）。用户不可编辑：预设来源是服务端记账，不写进 document';

COMMENT ON COLUMN public.blocks.preset_version IS '预设的版本号（外观/默认结构变了才涨），与组件协议版本分开（B2 迁移 676）';
