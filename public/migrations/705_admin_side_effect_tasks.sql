-- admin 域持久副作用任务表：提交后才能真正生效的外部副作用（策略重载、会话吊销）。
--
-- 这些副作用跨库或依赖进程内状态，无法和 PG 事务原子提交：事务回滚不了已经发出去的
-- Redis 删会话，也回滚不了别的实例内存里的策略副本。此前失败只能同步报错 + 打日志，
-- 日志不是可重放的持久事实，进程一重启就丢了。
--
-- 做法：在**同一个业务事务里**写一条任务行，提交后立即尝试幂等执行一次；
-- 成功标记 processed_time，失败保留 attempts / last_error 交给 admin 自己的 worker 重放。
-- 行随业务事务回滚 —— 业务没成，任务也不会留下。
--
-- 全局账号/策略副作用，与项目无关，故不带 project_id，也不参与 RLS 项目域过滤。
--
-- 幂等：CREATE TABLE / INDEX IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS admin_side_effect_tasks (
    id             bigserial    PRIMARY KEY,
    kind           text         NOT NULL,
    payload        text         NOT NULL,
    create_time    timestamptz  NOT NULL DEFAULT now(),
    claimed_time   timestamptz,
    processed_time timestamptz,
    attempts       integer      NOT NULL DEFAULT 0,
    last_error     text         NOT NULL DEFAULT ''
);

COMMENT ON TABLE admin_side_effect_tasks IS 'admin 提交后副作用的持久重放队列（策略重载 / 会话吊销）';
COMMENT ON COLUMN admin_side_effect_tasks.kind IS '副作用种类：policy_reload / revoke_session';
COMMENT ON COLUMN admin_side_effect_tasks.payload IS '执行该副作用需要的业务事实（JSON）';

-- worker 扫「未完成且租约已过期」的行；按 id 升序领取保证先进先出。
CREATE INDEX IF NOT EXISTS idx_admin_side_effect_tasks_pending
    ON admin_side_effect_tasks (id)
    WHERE processed_time IS NULL;
