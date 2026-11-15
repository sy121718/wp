-- table: presentation_dependencies（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.presentation_dependencies (
    presentation_id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    dependency_kind text NOT NULL,
    dependency_key text NOT NULL,
    revision text,
    last_checked timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT presentation_dependencies_dependency_kind_check CHECK ((dependency_kind = ANY (ARRAY['direct_content'::text, 'content_collection'::text, 'content_template'::text, 'menu'::text, 'media'::text, 'global_component'::text, 'site_setting'::text, 'runtime'::text, 'i18n'::text, 'block'::text, 'navigation'::text, 'page_target'::text, 'presentation_target'::text])))
);

-- @phase constr
ALTER TABLE ONLY public.presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_pkey PRIMARY KEY (artifact_id, dependency_kind, dependency_key);

-- @phase idx
CREATE INDEX idx_pres_deps_lookup ON public.presentation_dependencies USING btree (dependency_kind, dependency_key);

-- @phase fk
ALTER TABLE ONLY public.presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_artifact_id_presentation_id_fkey FOREIGN KEY (artifact_id, presentation_id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);
