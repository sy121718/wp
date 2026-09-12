-- 131 · 自动化流程（issue #38 P3）。
--
-- 三张表的分工：
--   mail_automations          流程**定义**（图：节点 + 连线）+ 触发方式，人写人改；
--   mail_automation_runs      运行**实例**（某个人走到哪一步、下次何时继续、为什么失败）；
--   mail_automation_node_logs 节点级**日志**（排障：这一步做了什么、跳去哪）；
--
-- 参考实现（Notifuse）把这三件事也分成 automation / automation_run / 节点执行日志，
-- 因为它们的生命周期完全不同：定义长期稳定、实例随人流动、日志量大且只读。

CREATE TABLE IF NOT EXISTS mail_automations (
    id              BIGSERIAL    PRIMARY KEY,
    name            VARCHAR(150) NOT NULL,
    description     VARCHAR(500),
    -- 触发方式：manual（手工添加某人）/ contact_created（注册）/ contact_subscribed（订阅）
    --          / email_opened / email_clicked / tag_added
    trigger_type    VARCHAR(32)  NOT NULL DEFAULT 'manual',
    -- 触发的附加条件（如 tag_added 要匹配哪个标签）
    trigger_params  JSONB,
    -- 图定义：{"entry":"n1","nodes":[{"key":"n1","type":"trigger","next":"n2"},...]}
    -- 用 JSONB 而不是关联表：图是**整体读写**的（编辑器一次保存整张图），拆表只会让读写更碎；
    -- 且节点形状各异，结构化列表达不了。这是「自由形状」的正当用法。
    definition      JSONB        NOT NULL,
    -- draft 草稿 / active 启用 / paused 暂停（暂停不影响已运行的实例）
    status          VARCHAR(16)  NOT NULL DEFAULT 'draft',
    -- 版本：实例记录启动时的版本，改图不影响正在跑的实例（否则它们会执行到不存在的节点）
    version         INTEGER      NOT NULL DEFAULT 1,
    create_by       BIGINT       NOT NULL DEFAULT 0,
    create_time     TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time     TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_mail_automations_status ON mail_automations (status);

CREATE TABLE IF NOT EXISTS mail_automation_runs (
    id                 BIGSERIAL    PRIMARY KEY,
    automation_id      BIGINT       NOT NULL,
    -- 启动时的流程版本：改图不该影响正在跑的实例
    automation_version INTEGER      NOT NULL DEFAULT 1,
    contact_id         BIGINT       NOT NULL,
    -- running 进行中 / waiting 等待中（延时节点）/ completed 完成 / failed 失败 / stopped 人工停止
    status             VARCHAR(16)  NOT NULL DEFAULT 'running',
    current_node       VARCHAR(64),
    -- 等待中的实例何时该被唤醒（延时节点）
    next_run_at        TIMESTAMP(3),
    error_message      TEXT,
    -- 哪个事件把人带进来的（排障时第一个要问的问题）
    trigger_event      VARCHAR(32),
    started_at         TIMESTAMP(3),
    finished_at        TIMESTAMP(3),
    create_time        TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time        TIMESTAMP(3)
);
-- 幂等：同一个人在同一条流程里**同时只能有一个进行中的实例**。
-- 没有这条约束，反复触发（开一次邮件、点一次链接）就会给同一个人发多次自动化邮件。
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_automation_runs_active
    ON mail_automation_runs (automation_id, contact_id)
    WHERE status IN ('running', 'waiting');
CREATE INDEX IF NOT EXISTS idx_mail_automation_runs_due
    ON mail_automation_runs (next_run_at) WHERE status = 'waiting';
CREATE INDEX IF NOT EXISTS idx_mail_automation_runs_contact ON mail_automation_runs (contact_id);

CREATE TABLE IF NOT EXISTS mail_automation_node_logs (
    id          BIGSERIAL    PRIMARY KEY,
    run_id      BIGINT       NOT NULL,
    node_key    VARCHAR(64)  NOT NULL,
    node_type   VARCHAR(32)  NOT NULL,
    -- ok 执行成功 / skipped 条件不满足跳过 / waiting 停在等待 / failed 失败
    status      VARCHAR(16)  NOT NULL,
    detail      TEXT,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mail_automation_node_logs_run ON mail_automation_node_logs (run_id, id);
