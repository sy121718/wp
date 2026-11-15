-- table: presentation_artifacts（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.presentation_artifacts (
    id uuid NOT NULL,
    presentation_instance_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    version bigint NOT NULL,
    source_hash text NOT NULL,
    build_input_hash text NOT NULL,
    artifact_provider text NOT NULL,
    artifact_key text NOT NULL,
    artifact_hash text NOT NULL,
    compiler_version text NOT NULL,
    registry_version text NOT NULL,
    manifest jsonb NOT NULL,
    payload_state text DEFAULT 'available'::text NOT NULL,
    payload_deleted_at timestamp with time zone,
    note text DEFAULT ''::text NOT NULL,
    created_by uuid NOT NULL,
    create_time timestamp with time zone CONSTRAINT presentation_artifacts_created_at_not_null NOT NULL,
    lang text DEFAULT 'zh-CN'::text NOT NULL,
    CONSTRAINT presentation_artifacts_payload_state_check CHECK ((payload_state = ANY (ARRAY['available'::text, 'gc_pending'::text, 'deleted'::text])))
);

CREATE UNIQUE INDEX uk_presentation_artifacts_instance_version_lang ON public.presentation_artifacts USING btree (presentation_instance_id, version, lang);

-- @phase constr
ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_id_presentation_instance_id_key UNIQUE (id, presentation_instance_id);

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_presentation_instance_id_fkey FOREIGN KEY (presentation_instance_id) REFERENCES public.presentation_instances(id);

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_snapshot_id_presentation_instance_i_fkey FOREIGN KEY (snapshot_id, presentation_instance_id) REFERENCES public.document_snapshots(id, presentation_instance_id);

-- @phase comment
COMMENT ON COLUMN public.presentation_artifacts.lang IS '构建语言（I18N-013）：唯一键第三维';
