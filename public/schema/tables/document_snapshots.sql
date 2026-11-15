-- table: document_snapshots（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.document_snapshots (
    id uuid NOT NULL,
    presentation_instance_id uuid NOT NULL,
    source_template_version_id uuid NOT NULL,
    source_entity_revision_id uuid NOT NULL,
    document jsonb NOT NULL,
    create_time timestamp with time zone CONSTRAINT document_snapshots_created_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_id_presentation_instance_id_key UNIQUE (id, presentation_instance_id);

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_presentation_instance_id_fkey FOREIGN KEY (presentation_instance_id) REFERENCES public.presentation_instances(id);

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_source_template_version_id_fkey FOREIGN KEY (source_template_version_id) REFERENCES public.content_template_versions(id);
