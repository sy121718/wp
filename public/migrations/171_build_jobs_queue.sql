-- 171_build_jobs_queue.sql
--
-- 审计 DB-007：build_jobs 表自 init_builder_schema 起就在，但 Go 侧零引用 —— 队列引擎未实现。
-- 迁移 069 明确说明「表在 init 中创建，队列引擎尚未接入 Go，非废弃」。本迁移把表补到可被消费：
--   1. 取任务的索引：按 status + created_at 取最老的一条待办；
--   2. 僵尸回收的索引：按 started_at 找超时的 running 行；
--   3. 「同一目标同一构建输入只排一次」的部分唯一索引 —— 依赖失效扇出会在一批里
--      反复标记同一个页面，没有它队列会被同一份工作填满。

CREATE INDEX IF NOT EXISTS idx_build_jobs_claim ON build_jobs (created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_build_jobs_running ON build_jobs (started_at) WHERE status = 'running';

-- 部分唯一索引的作用域是「未完成的任务」：已完成/失败的行不受影响，
-- 因此同一个页面可以反复入队（每次是新的一份工作），但同一时刻只排一份。
CREATE UNIQUE INDEX IF NOT EXISTS uq_build_jobs_pending
    ON build_jobs (source_type, source_id, build_input_hash) WHERE status = 'pending';

COMMENT ON TABLE build_jobs IS '构建任务队列（审计 DB-007）：worker 用 FOR UPDATE SKIP LOCKED 消费；状态 pending/running/superseded/failed/succeeded';
