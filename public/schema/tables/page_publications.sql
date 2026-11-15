-- table: page_publications（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_publications (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    active_path text NOT NULL,
    artifact_id uuid,
    artifact_hash text DEFAULT ''::text NOT NULL,
    published_at timestamp with time zone NOT NULL,
    update_time timestamp with time zone CONSTRAINT page_publications_updated_at_not_null NOT NULL,
    receipt_id bigint
);

-- @phase constr
ALTER TABLE ONLY public.page_publications
    ADD CONSTRAINT page_publications_pkey PRIMARY KEY (page_id, lang);

-- @phase comment
COMMENT ON TABLE public.page_publications IS '页面每语言激活状态（多语言 P3）：主键 (page_id, lang)，禁止空语言';

COMMENT ON COLUMN public.page_publications.artifact_hash IS '该语言当前激活产物的内容 hash（回滚/校验用）';

COMMENT ON COLUMN public.page_publications.receipt_id IS '成功激活该语言产物的发布回执 id（复用预登记回执 ID 作为 attempt 身份）；NULL = 历史行，收敛退既有判据';
