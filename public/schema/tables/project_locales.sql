-- table: project_locales（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.project_locales (
    project_id uuid NOT NULL,
    lang text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    create_time timestamp with time zone CONSTRAINT project_locales_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT project_locales_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.project_locales FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_project_locales_default ON public.project_locales USING btree (project_id) WHERE is_default;

CREATE POLICY project_isolation ON public.project_locales USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

ALTER TABLE public.project_locales ENABLE ROW LEVEL SECURITY;

-- @phase constr
ALTER TABLE ONLY public.project_locales
    ADD CONSTRAINT project_locales_pkey PRIMARY KEY (project_id, lang);

-- @phase comment
COMMENT ON TABLE public.project_locales IS '站点语言清单（多语言 P3）：顺序 + 默认标记 + 启用状态';

COMMENT ON COLUMN public.project_locales.is_default IS '站点默认语言（每工程至多一行，hreflang x-default 取它）';
