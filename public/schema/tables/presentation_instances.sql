-- table: presentation_instances（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.presentation_instances (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid NOT NULL,
    url_path text NOT NULL,
    template_id uuid NOT NULL,
    current_snapshot_id uuid,
    staged_snapshot_id uuid,
    staged_artifact_id uuid,
    active_artifact_id uuid,
    stale boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    published_at timestamp with time zone,
    create_time timestamp with time zone CONSTRAINT presentation_instances_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT presentation_instances_updated_at_not_null NOT NULL,
    instance_role text DEFAULT 'detail'::text NOT NULL,
    override_document jsonb,
    render_mode text DEFAULT 'template'::text NOT NULL,
    input_version bigint DEFAULT 0 NOT NULL,
    CONSTRAINT ck_presentation_instances_render_mode CHECK ((render_mode = ANY (ARRAY['template'::text, 'document'::text])))
);

ALTER TABLE ONLY public.presentation_instances FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_presentation_instances_entity_role ON public.presentation_instances USING btree (entity_type, entity_id, instance_role);

ALTER TABLE public.presentation_instances ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.presentation_instances USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_project_id_url_path_key UNIQUE (project_id, url_path);

-- @phase idx
CREATE INDEX idx_presentation_instances_project_role ON public.presentation_instances USING btree (project_id, instance_role);

-- @phase fk
ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_active_artifact_fk FOREIGN KEY (active_artifact_id, id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_snapshot_fk FOREIGN KEY (current_snapshot_id, id) REFERENCES public.document_snapshots(id, presentation_instance_id);

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_staged_artifact_fk FOREIGN KEY (staged_artifact_id, id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);

-- @phase comment
COMMENT ON COLUMN public.presentation_instances.override_document IS '实例级文档覆盖；NULL=跟随模板';

COMMENT ON COLUMN public.presentation_instances.render_mode IS '渲染模式 template=跟随绑定模板 | document=该商品独立文档（override_document）';

COMMENT ON COLUMN public.presentation_instances.input_version IS '实例编译输入代次：输入变更同事务 +1，激活只验证不推进';
