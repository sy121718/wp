-- table: page_dependencies（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_dependencies (
    page_id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    dependency_kind text NOT NULL,
    dependency_key text NOT NULL,
    revision text,
    last_checked timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT page_dependencies_dependency_kind_check CHECK ((dependency_kind = ANY (ARRAY['direct_content'::text, 'content_collection'::text, 'content_template'::text, 'menu'::text, 'media'::text, 'global_component'::text, 'site_setting'::text, 'runtime'::text, 'i18n'::text, 'block'::text, 'navigation'::text, 'page_target'::text, 'presentation_target'::text])))
);

-- @phase constr
ALTER TABLE ONLY public.page_dependencies
    ADD CONSTRAINT page_dependencies_pkey PRIMARY KEY (artifact_id, dependency_kind, dependency_key);

-- @phase idx
CREATE INDEX idx_page_deps_lookup ON public.page_dependencies USING btree (dependency_kind, dependency_key);

-- @phase fk
ALTER TABLE ONLY public.page_dependencies
    ADD CONSTRAINT page_dependencies_artifact_id_page_id_fkey FOREIGN KEY (artifact_id, page_id) REFERENCES public.page_artifacts(id, page_id);
