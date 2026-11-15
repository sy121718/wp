-- table: themes（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.themes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_active boolean DEFAULT false NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT themes_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT themes_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.themes FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_themes_project_active ON public.themes USING btree (project_id) WHERE (is_active = true);

CREATE UNIQUE INDEX uq_themes_project_name ON public.themes USING btree (project_id, name);

CREATE POLICY project_isolation ON public.themes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

ALTER TABLE public.themes ENABLE ROW LEVEL SECURITY;

-- @phase constr
ALTER TABLE ONLY public.themes
    ADD CONSTRAINT themes_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.themes
    ADD CONSTRAINT themes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;
