-- 161 · 访问统计与主数据审计保留期（IDX-001 / IDX-003 最小落地）。
--
-- 不拆分区（DB-004 留后续）：先加日汇总表 + 保留期配置列，供清理任务使用。

CREATE TABLE IF NOT EXISTS page_views_daily (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    day        date NOT NULL,
    path       text NOT NULL,
    views      bigint NOT NULL DEFAULT 0,
    visitors   bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, day, path)
);

CREATE INDEX IF NOT EXISTS idx_page_views_daily_project_day
    ON page_views_daily (project_id, day DESC);

-- 工程级保留天数（0 = 不自动清理，默认 90 天明细）。
ALTER TABLE projects ADD COLUMN IF NOT EXISTS analytics_retention_days integer NOT NULL DEFAULT 90;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS masterdata_retention_days integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN projects.analytics_retention_days IS 'page_views 明细保留天数；0=不清理';
COMMENT ON COLUMN projects.masterdata_retention_days IS 'master_data_changes 保留天数；0=不清理（append-only 默认永久）';
