-- table: sys_i18n_revision（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.sys_i18n_revision (
    id smallint DEFAULT 1 NOT NULL,
    revision bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone,
    CONSTRAINT ck_sys_i18n_revision_single CHECK ((id = 1))
);

-- @phase constr
ALTER TABLE ONLY public.sys_i18n_revision
    ADD CONSTRAINT sys_i18n_revision_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON TABLE public.sys_i18n_revision IS 'i18n 资源版本号（单行，单调递增）';

COMMENT ON COLUMN public.sys_i18n_revision.revision IS '资源版本号';

COMMENT ON COLUMN public.sys_i18n_revision.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
INSERT INTO public.sys_i18n_revision VALUES
	(1, 0, '2026-11-15 20:36:53.683+08') ON CONFLICT DO NOTHING;
