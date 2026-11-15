-- 764 · 补建 171 的取任务索引。
--
-- 171 的 CheckSQL 只判 uq_build_jobs_pending（历史原文，不改），idx_build_jobs_claim
-- 不在判定里：存量库有 uq 就判已应用，claim 可能从未创建 —— 队列取任务按
-- (创建时间) WHERE status='pending' 走，没有该索引就是全表扫。
--
-- idx_build_jobs_running 不在本条：295-build-jobs-lease 已经把它删掉（回收改按
-- 租约到期判定），终态不该存在。
--
-- 列名兼容：171 写的是 created_at，201_db019_db020_batch1.sql 之后改名 create_time。
-- 前提：uq 缺失意味着 171 从未成功执行过，该库也还没跑到 201（列名仍是 created_at）。
--
-- 幂等：CREATE INDEX IF NOT EXISTS。

DO $$
DECLARE
    ts_col text;
BEGIN
    SELECT column_name INTO ts_col
      FROM information_schema.columns
     WHERE table_schema = current_schema()
       AND table_name = 'build_jobs'
       AND column_name IN ('create_time', 'created_at')
     ORDER BY CASE column_name WHEN 'create_time' THEN 0 ELSE 1 END
     LIMIT 1;
    IF ts_col IS NULL THEN
        RAISE EXCEPTION 'build_jobs 缺少时间列（create_time / created_at）';
    END IF;

    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS idx_build_jobs_claim ON build_jobs (%I) WHERE status = %L',
        ts_col, 'pending');
END $$;
