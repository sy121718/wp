-- table: page_routes（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_routes (
    project_id uuid NOT NULL,
    path text NOT NULL,
    page_id uuid,
    presentation_id uuid,
    route_kind text NOT NULL,
    artifact_id uuid,
    update_time timestamp with time zone CONSTRAINT page_routes_updated_at_not_null NOT NULL,
    CONSTRAINT page_routes_check CHECK ((((page_id IS NOT NULL) AND (presentation_id IS NULL)) OR ((page_id IS NULL) AND (presentation_id IS NOT NULL)))),
    CONSTRAINT page_routes_route_kind_check CHECK ((route_kind = ANY (ARRAY['reserved'::text, 'active'::text, 'redirect'::text])))
);

ALTER TABLE ONLY public.page_routes FORCE ROW LEVEL SECURITY;

ALTER TABLE public.page_routes ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.page_routes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_pkey PRIMARY KEY (project_id, path);

-- @phase idx
CREATE INDEX idx_page_routes_project_kind_path ON public.page_routes USING btree (project_id, route_kind, path);

-- @phase fk
ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_presentation_id_fkey FOREIGN KEY (presentation_id) REFERENCES public.presentation_instances(id);

ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON INDEX public.idx_page_routes_project_kind_path IS '激活路由列举：按 kind 过滤 + path 排序（审计 IDX-020）';
