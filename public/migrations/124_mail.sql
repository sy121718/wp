-- 124_mail.sql
-- 邮箱模块基座（issue #37）：发信账号 / 模板 / 发送日志 / 抑制名单。
--
-- 设计取向（用户拍的板）：
--   · **一张表管所有发信配置**，系统邮件与营销邮件都在 mail_accounts 里，靠 purpose 分类；
--   · purpose **只是分类标记，不是使用限制** —— 配好 SMTP 的账号既能发事务邮件也能发营销；
--     群发/自动化时由操作者选账号（「默认事务」由 is_default 表达）；
--   · 配置**只从数据库读**，不做全局配置回落，避免两处配置打架；
--   · 后台入口在邮箱模块自己的页面，不塞进 /admin/settings ——
--     /admin/settings 存的是 projects.settings（**站点级** JSONB），而发信通道是全系统共用的，
--     放进去会变成「每个站点工程各配一份 SMTP」。
--
-- 不学 WordPress 生态的存法：MailPoet 的 wp_mailpoet_settings 是 name + value(longtext)，
-- 值是 PHP 序列化串（连发件人地址都埋在 a:2:{...} 里），无法索引、无法约束。
-- 这里每一项都是列。

-- 1. mail_accounts —— 发信账号（一条 = 一套可用的发信配置）
CREATE TABLE IF NOT EXISTS mail_accounts (
    id                 BIGSERIAL    PRIMARY KEY,
    -- 后台显示名（如「系统通知」「营销群发」）
    name               VARCHAR(64)  NOT NULL,
    -- 用途分类：transactional（默认，事务邮件）/ marketing（营销）。
    -- **仅作分类**：营销同样可以选用 transactional 账号（只要 SMTP 配好），
    -- 所以不要把它当权限或可用性判断。
    purpose            VARCHAR(16)  NOT NULL DEFAULT 'transactional',
    -- 该用途下的默认账号（服务层守卫同用途唯一）；事务邮件取它，营销由操作者选。
    is_default         BOOLEAN      NOT NULL DEFAULT FALSE,
    from_name          VARCHAR(100),
    from_email         VARCHAR(254) NOT NULL,
    reply_to           VARCHAR(254),
    -- 传输方式：smtp 起步，留 ses / mailgun / postmark / sendgrid 的位置
    provider           VARCHAR(32)  NOT NULL DEFAULT 'smtp',
    host               VARCHAR(255),
    port               INTEGER,
    username           VARCHAR(255),
    -- 密码 / API key **加密存储**（pkg/crypto）：绝不明文落库、绝不进日志
    password_cipher    TEXT,
    -- 加密方式：none / ssl / starttls
    encryption         VARCHAR(16)  NOT NULL DEFAULT 'starttls',
    -- 每小时发送上限（营销节流用）；0 = 不限
    rate_per_hour      INTEGER      NOT NULL DEFAULT 0,
    status             SMALLINT     NOT NULL DEFAULT 1,
    -- 后台「测试发送」的结论留痕：配置对不对、失败原因是什么
    last_check_at      TIMESTAMP(3),
    last_check_error   TEXT,
    create_time        TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time        TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_mail_accounts_purpose ON mail_accounts (purpose);
-- 每种用途最多一个默认账号（部分唯一索引兜底，服务层也有守卫）
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_accounts_default ON mail_accounts (purpose) WHERE is_default;

-- 2. mail_templates —— 邮件模板
--
-- 模板 key 是**稳定标识**（如 register_verify / password_reset），代码按 key 引用；
-- 改文案只改模板行，不动代码。locale 为空表示通用兜底。
CREATE TABLE IF NOT EXISTS mail_templates (
    id          BIGSERIAL    PRIMARY KEY,
    template_key VARCHAR(64) NOT NULL,
    locale      VARCHAR(16)  NOT NULL DEFAULT '',
    name        VARCHAR(100) NOT NULL,
    subject     VARCHAR(255) NOT NULL,
    body_html   TEXT         NOT NULL DEFAULT '',
    body_text   TEXT         NOT NULL DEFAULT '',
    -- 该模板声明会用到的变量（JSONB 数组）：发送前校验，避免「变量拼错静默发出去几千封」
    variables   TEXT[]       NOT NULL DEFAULT '{}',
    status      SMALLINT     NOT NULL DEFAULT 1,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_templates_key_locale ON mail_templates (template_key, locale);

-- 3. mail_logs —— 发送记录（事务邮件必须可追溯、可重发）
CREATE TABLE IF NOT EXISTS mail_logs (
    id            BIGSERIAL    PRIMARY KEY,
    account_id    BIGINT,
    template_key  VARCHAR(64),
    to_email      VARCHAR(254) NOT NULL,
    subject       VARCHAR(255),
    -- pending / sent / failed / suppressed
    status        VARCHAR(16)  NOT NULL DEFAULT 'pending',
    provider      VARCHAR(32),
    provider_msg_id VARCHAR(191),
    -- 错误分类（学 Notifuse 的 emailerror）：temporary 可重试 / permanent 硬退信 / configuration 需人介入
    error_kind    VARCHAR(16),
    error_message TEXT,
    retry_count   INTEGER      NOT NULL DEFAULT 0,
    sent_at       TIMESTAMP(3),
    create_time   TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mail_logs_to_email ON mail_logs (to_email);
CREATE INDEX IF NOT EXISTS idx_mail_logs_status ON mail_logs (status);
CREATE INDEX IF NOT EXISTS idx_mail_logs_create_time ON mail_logs (create_time DESC);

-- 4. mail_suppressions —— 抑制名单（**发送前必查**）
--
-- 退订 / 硬退信 / 投诉的地址进这里。不查它的后果：一直往坏地址发，把发信域名声誉打烂。
CREATE TABLE IF NOT EXISTS mail_suppressions (
    id          BIGSERIAL    PRIMARY KEY,
    email       VARCHAR(254) NOT NULL,
    -- unsubscribe（用户主动退订）/ hard_bounce（地址不存在）/ complaint（标记垃圾邮件）/ manual（后台手工加）
    reason      VARCHAR(16)  NOT NULL,
    source      VARCHAR(64),
    note        VARCHAR(255),
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_suppressions_email ON mail_suppressions (lower(email));
