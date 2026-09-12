-- 125_mail_marketing.sql
-- 营销域：联系人 / 列表 / 成员 / 活动 / 事件（issue #37 的营销部分）。
--
-- 域划分参考了两套独立实现（Notifuse 的 internal/domain 与 MailPoet 的表），它们长出同样的域：
--   · 联系人（Notifuse contact / MailPoet subscribers）**独立于系统用户** ——
--     访客可以只订阅邮件、不注册账号；注册用户也未必订阅。所以本表与 users 是**可选的关联**
--     而不是依赖（user_id 可空）。
--   · 列表 + 成员（多对多）
--   · 活动（broadcast）与自动化（automation）是两种东西：活动是「选人群 → 发一次 → 结束」，
--     自动化是「触发 → 走流程 → 多步多时机」。本期只做活动，自动化留后续。
--
-- **同意状态是这张表的核心**（status / subscribed_at / consent_source 三件套）：
-- 用户明确说联系人有两个来源 —— 拉系统客户、自己导入。但「拉过来」≠「同意收营销邮件」：
-- 未经同意就群发，收件方会标记垃圾邮件，发信域名声誉被打烂（之后连事务邮件都进垃圾箱）。
-- 所以：
--   · 从系统用户拉 → source=system_user，默认 status=pending（待确认），
--     只有在用户侧明确勾选过接收营销（user_preferences.email_notify）时才置 subscribed；
--   · 导入 → source=import，默认同样 pending；导入时操作者若声明「这批人已同意」并填写来源，
--     才置 subscribed 并记 consent_source。
-- 硬退信与投诉进抑制名单（mail_suppressions），且状态改为 bounced / complained。

-- 1. mail_contacts —— 营销联系人（独立实体）
CREATE TABLE IF NOT EXISTS mail_contacts (
    id              BIGSERIAL    PRIMARY KEY,
    email           VARCHAR(254) NOT NULL,
    name            VARCHAR(128),
    -- 可选关联系统用户：从系统客户拉的会有值；导入的、纯订阅的为空。
    -- **不设外键**：用户注销（软删除）后联系人仍在，营销历史不该被级联。
    user_id         BIGINT,
    -- 来源：system_user（拉系统客户）/ import（导入）/ manual（后台手工）/ subscribe（前台订阅）
    source          VARCHAR(16)  NOT NULL DEFAULT 'manual',
    -- 同意状态：pending 待确认 / subscribed 已订阅（可发营销） / unsubscribed 已退订 /
    -- bounced 硬退信 / complained 投诉。**只有 subscribed 才允许发营销**。
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    -- 明确同意的时间与来源（合规留痕，退订/投诉时保留原值以便追溯）
    subscribed_at   TIMESTAMP(3),
    consent_source  VARCHAR(64),
    -- 导入来的杂字段与自定义属性（结构化字段放列，其余进这里，与 user_meta 同一判据）
    attributes      JSONB,
    last_activity_at TIMESTAMP(3),
    create_time     TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time     TIMESTAMP(3)
);
-- 一个邮箱一个联系人（大小写不敏感）
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_contacts_email ON mail_contacts (lower(email));
CREATE INDEX IF NOT EXISTS idx_mail_contacts_status ON mail_contacts (status);
CREATE INDEX IF NOT EXISTS idx_mail_contacts_user_id ON mail_contacts (user_id) WHERE user_id IS NOT NULL;

-- 2. mail_lists —— 列表
CREATE TABLE IF NOT EXISTS mail_lists (
    id          BIGSERIAL    PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    description VARCHAR(255),
    -- 前台订阅表单可用的短标识（为空则不可被前台订阅）
    slug        VARCHAR(100),
    status      SMALLINT     NOT NULL DEFAULT 1,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_lists_slug ON mail_lists (slug) WHERE slug IS NOT NULL AND slug <> '';

-- 3. mail_list_members —— 列表成员（联系人 × 列表，多对多）
CREATE TABLE IF NOT EXISTS mail_list_members (
    id          BIGSERIAL    PRIMARY KEY,
    list_id     BIGINT       NOT NULL,
    contact_id  BIGINT       NOT NULL,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_list_members_pair ON mail_list_members (list_id, contact_id);
CREATE INDEX IF NOT EXISTS idx_mail_list_members_contact ON mail_list_members (contact_id);

-- 4. mail_campaigns —— 群发活动
CREATE TABLE IF NOT EXISTS mail_campaigns (
    id            BIGSERIAL    PRIMARY KEY,
    name          VARCHAR(150) NOT NULL,
    -- 用哪个发信账号（用户拍板：配置好 SMTP 的账号既能发事务也能发营销，这里显式选）
    account_id    BIGINT       NOT NULL,
    template_id   BIGINT       NOT NULL,
    -- 投递目标列表（本期单列表；多列表将来加关联表，不改本表语义）
    list_id       BIGINT       NOT NULL,
    subject       VARCHAR(255) NOT NULL,
    -- 发送时覆盖模板变量（JSONB 对象）
    variables     JSONB,
    -- draft 草稿 / scheduled 待发 / sending 发送中 / sent 已完成 / failed 失败 / canceled 已取消
    status        VARCHAR(16)  NOT NULL DEFAULT 'draft',
    scheduled_at  TIMESTAMP(3),
    started_at    TIMESTAMP(3),
    finished_at   TIMESTAMP(3),
    -- 计数快照（发送过程中递增，列表页直接读，不必每次聚合事件表）
    total_count   INTEGER      NOT NULL DEFAULT 0,
    sent_count    INTEGER      NOT NULL DEFAULT 0,
    failed_count  INTEGER      NOT NULL DEFAULT 0,
    create_by     BIGINT       NOT NULL DEFAULT 0,
    create_time   TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time   TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_mail_campaigns_status ON mail_campaigns (status);

-- 5. mail_campaign_events —— 打开 / 点击 / 退信 / 退订事件
CREATE TABLE IF NOT EXISTS mail_campaign_events (
    id          BIGSERIAL    PRIMARY KEY,
    campaign_id BIGINT       NOT NULL,
    contact_id  BIGINT       NOT NULL,
    -- open 打开 / click 点击 / bounce 退信 / unsubscribe 退订 / complaint 投诉
    event_type  VARCHAR(16)  NOT NULL,
    url         VARCHAR(1000),
    ip          VARCHAR(50),
    user_agent  VARCHAR(255),
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mail_campaign_events_campaign ON mail_campaign_events (campaign_id, event_type);
CREATE INDEX IF NOT EXISTS idx_mail_campaign_events_contact ON mail_campaign_events (contact_id);
