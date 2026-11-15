-- table: page_artifact_objects（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_artifact_objects (
    artifact_id uuid NOT NULL,
    content_hash text NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_pkey PRIMARY KEY (artifact_id, content_hash);

-- @phase fk
ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.page_artifacts(id);

ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_content_hash_fkey FOREIGN KEY (content_hash) REFERENCES public.content_objects(content_hash) ON DELETE CASCADE;
