-- table: presentation_publications（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.presentation_publications (
    presentation_id uuid NOT NULL,
    lang text NOT NULL,
    active_path text NOT NULL,
    artifact_id uuid,
    artifact_hash text DEFAULT ''::text NOT NULL,
    published_at timestamp with time zone NOT NULL,
    update_time timestamp with time zone CONSTRAINT presentation_publications_updated_at_not_null NOT NULL,
    receipt_id bigint
);

-- @phase constr
ALTER TABLE ONLY public.presentation_publications
    ADD CONSTRAINT presentation_publications_pkey PRIMARY KEY (presentation_id, lang);

-- @phase fk
ALTER TABLE ONLY public.presentation_publications
    ADD CONSTRAINT presentation_publications_presentation_id_fkey FOREIGN KEY (presentation_id) REFERENCES public.presentation_instances(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.presentation_publications IS '自动发布实例每语言激活状态（I18N-013）';

COMMENT ON COLUMN public.presentation_publications.receipt_id IS '成功激活该语言产物的发布回执 id（复用预登记回执 ID 作为 attempt 身份）；NULL = 历史行，收敛退 artifact_id 判据';
