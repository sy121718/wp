-- table: page_stagings（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_stagings (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    artifact_id uuid NOT NULL,
    artifact_hash text DEFAULT ''::text NOT NULL,
    draft_version bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone CONSTRAINT page_stagings_updated_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.page_stagings
    ADD CONSTRAINT page_stagings_pkey PRIMARY KEY (page_id, lang);

-- @phase comment
COMMENT ON TABLE public.page_stagings IS '页面每语言暂存产物（多语言 P3）：主键 (page_id, lang)';
