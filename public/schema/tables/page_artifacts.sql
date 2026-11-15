-- table: page_artifacts（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_artifacts (
    id uuid NOT NULL,
    page_id uuid NOT NULL,
    version bigint NOT NULL,
    source_document jsonb NOT NULL,
    page_document_schema_version integer NOT NULL,
    source_hash text NOT NULL,
    build_input_manifest jsonb,
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
    create_time timestamp with time zone CONSTRAINT page_artifacts_created_at_not_null NOT NULL,
    lang text DEFAULT 'zh-CN'::text NOT NULL,
    CONSTRAINT page_artifacts_payload_state_check CHECK ((payload_state = ANY (ARRAY['available'::text, 'gc_pending'::text, 'deleted'::text])))
);

CREATE UNIQUE INDEX uk_page_artifacts_page_hash ON public.page_artifacts USING btree (page_id, artifact_hash);

-- @phase constr
ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_id_page_id_key UNIQUE (id, page_id);

ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_page_artifacts_gc ON public.page_artifacts USING btree (create_time) WHERE (payload_state = 'available'::text);

CREATE INDEX idx_page_artifacts_registry ON public.page_artifacts USING btree (registry_version) WHERE (payload_state = 'available'::text);

-- @phase fk
ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);

-- @phase comment
COMMENT ON COLUMN public.page_artifacts.build_input_manifest IS 'DB-02 遗留列：历史上被写入与 manifest 同字节的输出清单，零读取者。305 起写入侧不再写它（新行 NULL），存量字节保留待后续迁移删除；读取清单请用 manifest 列。';

COMMENT ON COLUMN public.page_artifacts.lang IS '产物构建语言；同页同哈希归档幂等，不同内容各有不可变产物身份';
