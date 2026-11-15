-- table: content_templates（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.content_templates (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    entity_type text NOT NULL,
    draft_document jsonb NOT NULL,
    draft_version bigint DEFAULT 1 NOT NULL,
    current_version_id uuid,
    create_time timestamp with time zone CONSTRAINT content_templates_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT content_templates_updated_at_not_null NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    template_role text DEFAULT 'detail'::text NOT NULL
);

ALTER TABLE ONLY public.content_templates FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX idx_content_templates_default_per_project_type ON public.content_templates USING btree (project_id, entity_type) WHERE (is_default = true);

ALTER TABLE public.content_templates ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.content_templates USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.content_templates
    ADD CONSTRAINT content_templates_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_content_templates_project_role ON public.content_templates USING btree (project_id, entity_type, template_role);

-- @phase fk
ALTER TABLE ONLY public.content_templates
    ADD CONSTRAINT content_templates_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);
