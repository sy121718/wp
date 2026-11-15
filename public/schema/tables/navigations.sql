-- table: navigations（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.navigations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    title text NOT NULL,
    path text NOT NULL,
    kind text DEFAULT 'header'::text NOT NULL,
    parent_id uuid,
    sort_order integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT navigations_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT navigations_updated_at_not_null NOT NULL,
    source_type text DEFAULT 'custom'::text NOT NULL,
    source_id uuid,
    target text DEFAULT 'self'::text NOT NULL,
    panel_block_id uuid,
    panel_width text DEFAULT 'auto'::text NOT NULL,
    CONSTRAINT navigations_kind_check CHECK ((kind = ANY (ARRAY['header'::text, 'header_mobile'::text, 'footer'::text, 'footer_mobile'::text]))),
    CONSTRAINT navigations_panel_width_check CHECK ((panel_width = ANY (ARRAY['auto'::text, 'full'::text]))),
    CONSTRAINT navigations_source_type_check CHECK ((source_type = ANY (ARRAY['custom'::text, 'page'::text, 'article'::text, 'product'::text, 'category'::text, 'block'::text]))),
    CONSTRAINT navigations_target_check CHECK ((target = ANY (ARRAY['self'::text, 'blank'::text])))
);

ALTER TABLE ONLY public.navigations FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_navigation_project_kind_path ON public.navigations USING btree (project_id, kind, path);

ALTER TABLE public.navigations ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.navigations USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_navigations_project ON public.navigations USING btree (project_id, kind, sort_order);

CREATE INDEX idx_navigations_source ON public.navigations USING btree (source_type, source_id);

-- @phase fk
ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_panel_block_id_fkey FOREIGN KEY (panel_block_id) REFERENCES public.blocks(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.navigations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.navigations IS '公开站点导航（0-C，与后台 menu 严格隔离）';

COMMENT ON COLUMN public.navigations.kind IS '菜单位置：header/header_mobile/footer/footer_mobile（桌面与移动端各自独立）';

COMMENT ON COLUMN public.navigations.source_type IS '菜单项来源：custom/page/article/product/category/block';

COMMENT ON COLUMN public.navigations.source_id IS '来源实体 ID（custom 时为空，直接用 title/path）';

COMMENT ON COLUMN public.navigations.target IS '打开方式：self（当前窗口）/ blank（新窗口）';

COMMENT ON COLUMN public.navigations.panel_block_id IS '悬浮面板引用的全局块（NULL=无面板，超级菜单）';

COMMENT ON COLUMN public.navigations.panel_width IS '面板展示宽度 auto=跟随内容 / full=通栏';
