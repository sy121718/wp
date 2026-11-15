-- table: content_template_versions（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.content_template_versions (
    id uuid NOT NULL,
    template_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    source_hash text NOT NULL,
    created_by uuid NOT NULL,
    create_time timestamp with time zone CONSTRAINT content_template_versions_created_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_id_template_id_key UNIQUE (id, template_id);

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_template_id_version_key UNIQUE (template_id, version);

-- @phase fk
ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);
