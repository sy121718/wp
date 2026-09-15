-- 170_analytics_rollup.sql
--
-- 审计 DB-005（analytics 汇总每次全量 GROUP BY）与 IDX-010（路径排行 COUNT DISTINCT + 深分页）。
--
-- 161 已经建了日汇总表 page_views_daily，注释写明「先加日汇总表……供清理任务使用」，
-- 但它**从未被任何代码消费**（零写入、零读取）。本迁移不新建第二张语义重复的表，
-- 而是把它补齐到能真正承担汇总职责：
--   1. 加 scope：'all' 行存当天工程级**精确** UV。路径级 UV 相加不等于工程级 UV
--      （同一访客访问两个路径会被计两次），所以工程级必须单独一行，不能从路径级汇总出来；
--   2. 加 rolled_at：记录该行最后一次重算时间，运维据此判断汇总任务是否在跑；
--   3. 主键扩到 (project_id, day, scope, path)：三列主键容不下 scope 维；
--   4. 加路径排行索引，游标分页沿它走。

ALTER TABLE page_views_daily ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'path';
ALTER TABLE page_views_daily ADD COLUMN IF NOT EXISTS rolled_at timestamptz NOT NULL DEFAULT now();

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'page_views_daily_scope_check'
          AND conrelid = 'page_views_daily'::regclass
    ) THEN
        ALTER TABLE page_views_daily
            ADD CONSTRAINT page_views_daily_scope_check CHECK (scope IN ('all', 'path'));
    END IF;
END $$;

-- 主键必须在 scope 上：同一 (project, day, path) 要能同时存 all 与 path 两行。
-- 换主键前先删旧约束（IF EXISTS），再加四列主键；判定条件是「旧主键里没有 scope 列」。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint c
        WHERE c.conname = 'page_views_daily_pkey'
          AND c.conrelid = 'page_views_daily'::regclass
          AND 'scope'::name = ANY (
              SELECT a.attname FROM pg_attribute a
              WHERE a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey))
    ) THEN
        ALTER TABLE page_views_daily DROP CONSTRAINT IF EXISTS page_views_daily_pkey;
        ALTER TABLE page_views_daily ADD PRIMARY KEY (project_id, day, scope, path);
    END IF;
END $$;

-- 路径排行（IDX-010）：排序与游标都走这条索引，深分页成本不随页码增长。
CREATE INDEX IF NOT EXISTS idx_page_views_daily_rank
    ON page_views_daily (project_id, day, scope, path);

COMMENT ON TABLE page_views_daily IS '按天预聚合的访问统计（审计 DB-005/IDX-010）：历史窗口读这里，page_views 明细只负责当天与下钻';
COMMENT ON COLUMN page_views_daily.scope IS 'all=工程级当日总计（path 固定空串）；path=路径级当日聚合';
COMMENT ON COLUMN page_views_daily.rolled_at IS '该行最后一次重算时间（汇总任务每次全量重算当天）';
