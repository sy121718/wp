-- table: page_revisions（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_revisions (
    id uuid NOT NULL,
    page_id uuid NOT NULL,
    version bigint NOT NULL,
    draft_path text NOT NULL,
    draft_document jsonb NOT NULL,
    source_hash text NOT NULL,
    create_time timestamp with time zone CONSTRAINT page_revisions_created_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_page_id_version_key UNIQUE (page_id, version);

ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);
