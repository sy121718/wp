-- table: page_views_daily（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_views_daily (
    project_id uuid NOT NULL,
    day date NOT NULL,
    path text NOT NULL,
    views bigint DEFAULT 0 NOT NULL,
    visitors bigint DEFAULT 0 NOT NULL,
    scope text DEFAULT 'path'::text NOT NULL,
    rolled_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT page_views_daily_scope_check CHECK ((scope = ANY (ARRAY['all'::text, 'path'::text])))
);

ALTER TABLE ONLY public.page_views_daily FORCE ROW LEVEL SECURITY;

ALTER TABLE public.page_views_daily ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.page_views_daily USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.page_views_daily
    ADD CONSTRAINT page_views_daily_pkey PRIMARY KEY (project_id, day, scope, path);

-- @phase idx
CREATE INDEX idx_page_views_daily_project_day ON public.page_views_daily USING btree (project_id, day DESC);

CREATE INDEX idx_page_views_daily_rank ON public.page_views_daily USING btree (project_id, day, scope, path);

-- @phase fk
ALTER TABLE ONLY public.page_views_daily
    ADD CONSTRAINT page_views_daily_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.page_views_daily IS '按天预聚合的访问统计（审计 DB-005/IDX-010）：历史窗口读这里，page_views 明细只负责当天与下钻';

COMMENT ON COLUMN public.page_views_daily.scope IS 'all=工程级当日总计（path 固定空串）；path=路径级当日聚合';

COMMENT ON COLUMN public.page_views_daily.rolled_at IS '该行最后一次重算时间（汇总任务每次全量重算当天）';
