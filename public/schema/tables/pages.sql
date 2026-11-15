-- table: pages（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.pages (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    kind text NOT NULL,
    content_target_type text NOT NULL,
    content_target_id uuid,
    source_blueprint_version_id uuid,
    draft_path text NOT NULL,
    active_path text,
    draft_document jsonb NOT NULL,
    draft_version bigint DEFAULT 1 NOT NULL,
    staged_artifact_id uuid,
    active_artifact_id uuid,
    stale boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    published_at timestamp with time zone,
    create_time timestamp with time zone CONSTRAINT pages_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT pages_updated_at_not_null NOT NULL,
    theme_id uuid,
    rebuild_failed_at timestamp with time zone,
    rebuild_failed_stage character varying(16),
    excluded_langs text[] DEFAULT '{}'::text[] NOT NULL,
    input_version bigint DEFAULT 0 NOT NULL,
    CONSTRAINT pages_content_contract_check CHECK ((((kind = ANY (ARRAY['home'::text, 'archive'::text, 'search'::text, 'notFound'::text])) AND (content_target_type = 'none'::text) AND (content_target_id IS NULL)) OR ((kind = 'page'::text) AND (content_target_type = 'page'::text) AND (content_target_id IS NOT NULL)) OR ((kind = 'article'::text) AND (content_target_type = 'article'::text) AND (content_target_id IS NOT NULL)) OR ((kind = 'tag'::text) AND (content_target_type = 'tag'::text) AND (content_target_id IS NOT NULL))))
);

ALTER TABLE ONLY public.pages FORCE ROW LEVEL SECURITY;

ALTER TABLE public.pages ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.pages USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_id_project_id_key UNIQUE (id, project_id);

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_pages_blockref ON public.pages USING gin (jsonb_path_query_array(draft_document, '$.**."blockId"'::jsonpath)) WHERE (deleted_at IS NULL);

CREATE INDEX idx_pages_structure_footer ON public.pages USING btree (((((draft_document -> 'settings'::text) -> 'structure'::text) ->> 'footerBlockId'::text))) WHERE (deleted_at IS NULL);

CREATE INDEX idx_pages_structure_header ON public.pages USING btree (((((draft_document -> 'settings'::text) -> 'structure'::text) ->> 'headerBlockId'::text))) WHERE (deleted_at IS NULL);

CREATE INDEX idx_pages_theme_alive ON public.pages USING btree (theme_id, update_time DESC) WHERE (deleted_at IS NULL);

-- @phase fk
ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_active_artifact_fk FOREIGN KEY (active_artifact_id, id) REFERENCES public.page_artifacts(id, page_id);

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_source_blueprint_version_id_fkey FOREIGN KEY (source_blueprint_version_id) REFERENCES public.blueprint_versions(id);

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_staged_artifact_fk FOREIGN KEY (staged_artifact_id, id) REFERENCES public.page_artifacts(id, page_id);

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_theme_id_fkey FOREIGN KEY (theme_id) REFERENCES public.themes(id) ON DELETE SET NULL;

-- @phase comment
COMMENT ON COLUMN public.pages.excluded_langs IS '本页排除的语言（完整语言码）：不产出、不参与语言切换器 / hreflang / sitemap；空数组 = 全部站点语言都产出';

COMMENT ON COLUMN public.pages.input_version IS '页面编译输入代次：输入变更（草稿/译文扇出/依赖标记）同事务 +1，激活只验证不推进';

COMMENT ON INDEX public.idx_pages_blockref IS '块引用（core.globalref 的 blockId）表达式 GIN 索引，仅存活页面；表达式须与 page_model.go blockRefMatchCond 一致';

COMMENT ON INDEX public.idx_pages_structure_footer IS 'settings.structure 页脚绑定（footerBlockId）表达式索引，仅存活页面';

COMMENT ON INDEX public.idx_pages_structure_header IS 'settings.structure 页眉绑定（headerBlockId）表达式索引，仅存活页面';

COMMENT ON INDEX public.idx_pages_theme_alive IS '按主题刷新/重挂与主题内页面列表，仅存活页面（部分索引 deleted_at IS NULL）';
