-- 295 · build_jobs 的任务租约、来源互斥与完成归属（审计 DB-01）。
--
-- 复现的三个缺陷（真实 PostgreSQL）：
--   ① 同输入第一条 running 之后仍可再入队一条 pending —— 171 的部分唯一索引只覆盖
--      status='pending'，running 期间没有占用去重键；
--   ② 回收把陈旧 running 无条件改回 pending，撞上 171 的 uq_build_jobs_pending 报 23505，
--      **整条回收语句**失败：一条坏任务会把其它本该被回收的任务一起拖住；
--   ③ 完成写入只按 id 匹配，被回收并重新认领后，旧 worker 的完成结果会覆盖新 worker 的状态。
--
-- 本迁移补齐四类持久化形状，配套的判定逻辑在 internal/module/build：
--   project_id / lang / intent —— 任务的作用域与来源显式入库（消费侧不必再反推），
--     intent 目前取 manual（人工发起）/ dependency（依赖失效自动重建）两类，
--     当前只有依赖重建这一条生产者，ARCH-04 的多语言与重新发布语义不在本批次；
--   lease_token / lease_expires_time —— 认领时原子产生，完成写入必须带上同一个 token；
--   attempt —— 认领次数，供后台判断「这条任务被反复领取」。
--
-- 幂等：列用 IF NOT EXISTS；约束先 DROP 再 ADD；索引用 IF NOT EXISTS；
-- 存量归并 / 回填都带状态或空值限定，可重复执行。

ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS project_id uuid NULL;
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS lang text NOT NULL DEFAULT '';
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS intent text NOT NULL DEFAULT 'dependency';
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS lease_token uuid NULL;
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS lease_expires_time timestamptz NULL;
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS attempt integer NOT NULL DEFAULT 0;

ALTER TABLE build_jobs DROP CONSTRAINT IF EXISTS build_jobs_intent_check;
ALTER TABLE build_jobs ADD CONSTRAINT build_jobs_intent_check
    CHECK (intent IN ('manual', 'dependency'));

-- 存量 running 行补一个等价租约：旧实现按 started_at + 15 分钟判定僵尸，
-- 这里把同一判据写成显式到期时间，避免升级瞬间的「在途任务」永远不被回收
-- （回收谓词只看 lease_expires_time，不给旧行补这一列它们会被跳过）。
UPDATE build_jobs
   SET lease_expires_time = COALESCE(started_at, create_time) + interval '15 minutes'
 WHERE status = 'running' AND lease_expires_time IS NULL;

-- 存量重复 running 归并：旧实现的 claim 只锁 job 行，同一来源可以同时跑两条。
-- 保留 id 最大（最新认领）的一条，其余标 superseded —— 队列里被判定为「没有意义的重复工作」
-- 本来就是 superseded 的语义，这与「冲突打回给人」不矛盾：这里要让出的不是业务数据，
-- 而是一份可以由存活那条重新做出来的构建工作。
UPDATE build_jobs b
   SET status = 'superseded',
       lease_token = NULL,
       lease_expires_time = NULL,
       completed_at = now(),
       error_message = COALESCE(b.error_message, '同来源重复 running（旧实现遗留）：保留最新一条，本行合并为 superseded')
 WHERE b.status = 'running'
   AND EXISTS (
       SELECT 1 FROM build_jobs n
        WHERE n.status = 'running'
          AND n.source_type = b.source_type
          AND n.source_id = b.source_id
          AND n.id > b.id
   );

-- 来源互斥由数据库层保证：同一来源同一时刻最多一条 running。
-- 选唯一索引而不是「来源租约行」的理由见 internal/module/build/model/build_model.go 的 claimSQL 注释：
-- 本队列的互斥窗口恰好就是 running 期间，索引已经把这条不变量表达完整，
-- 不必再养一张需要独立清理与过期的租约表。
CREATE UNIQUE INDEX IF NOT EXISTS uq_build_jobs_running_source
    ON build_jobs (source_type, source_id) WHERE status = 'running';

-- 回收改按租约到期时间判定，idx_build_jobs_running(started_at) 从此零命中，直接删掉
-- （留着只会给每次 running 状态写入多付一次索引维护）。
DROP INDEX IF EXISTS idx_build_jobs_running;
CREATE INDEX IF NOT EXISTS idx_build_jobs_lease_expires
    ON build_jobs (lease_expires_time) WHERE status = 'running';

-- 存量任务回填工程作用域（带 to_regclass 守卫：这两张表在本迁移之前早已存在，
-- 守卫只是让「只跑了部分迁移的库」不至于整条失败）。回填不改变任务状态，只把
-- 「这条工作属于哪个工程」从隐式变成显式。
DO $$
BEGIN
    IF to_regclass('pages') IS NOT NULL THEN
        UPDATE build_jobs j
           SET project_id = p.project_id
          FROM pages p
         WHERE j.project_id IS NULL
           AND j.source_type = 'page'
           AND j.source_id = p.id;
    END IF;
    IF to_regclass('presentation_instances') IS NOT NULL THEN
        UPDATE build_jobs j
           SET project_id = i.project_id
          FROM presentation_instances i
         WHERE j.project_id IS NULL
           AND j.source_type = 'presentation'
           AND j.source_id = i.id;
    END IF;
END $$;

COMMENT ON COLUMN build_jobs.project_id IS '任务所属站点工程（入队时显式写入；RLS 铺到本表前的显式作用域来源）';
COMMENT ON COLUMN build_jobs.lang IS '构建语言；ARCH-04 的多语言任务才填具体语言，当前生产者一律为空串（默认语言）';
COMMENT ON COLUMN build_jobs.intent IS '构建意图：manual=人工发起，dependency=依赖失效自动重建';
COMMENT ON COLUMN build_jobs.lease_token IS '认领时原子产生的租约令牌；完成 / 失败写入必须带回同一个 token';
COMMENT ON COLUMN build_jobs.lease_expires_time IS '租约到期时间；到期后由回收流程合并或退回 pending';
COMMENT ON COLUMN build_jobs.attempt IS '认领次数（回收后重新认领会累加）';

COMMENT ON TABLE build_jobs IS '构建任务队列（审计 DB-007 / DB-01）：worker 用 FOR UPDATE SKIP LOCKED claim 并拿到租约令牌；同一来源同时最多一条 running（uq_build_jobs_running_source）；状态 pending/running/superseded/failed/succeeded';
