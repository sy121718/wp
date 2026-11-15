-- table: projects（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.projects (
    id uuid NOT NULL,
    name text NOT NULL,
    settings jsonb NOT NULL,
    create_time timestamp with time zone CONSTRAINT projects_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT projects_updated_at_not_null NOT NULL,
    analytics_retention_days integer DEFAULT 90 NOT NULL,
    masterdata_retention_days integer DEFAULT 0 NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON COLUMN public.projects.analytics_retention_days IS 'page_views 明细保留天数；0=不清理';

COMMENT ON COLUMN public.projects.masterdata_retention_days IS 'master_data_changes 保留天数；0=不清理（append-only 默认永久）';
