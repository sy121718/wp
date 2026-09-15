-- 199_webhook.sql
-- 插件 webhook 外部集成通道（OSS-006）+ SSRF 防护（SEC-015）。
--
-- 设计取向：
--   · webhook_endpoints 是**预注册白名单**：只有管理员在这里登记过的
--     「事件类型 × 目标 URL」组合才允许外发，事件发布方没有指定 URL 的入口；
--   · secret_cipher 加密存储（pkg/crypto，装配期注入 app.secret 加密）：
--     HMAC-SHA256 签名密钥绝不明文落库；
--   · webhook_deliveries 一行 = 一次投递：入队即建 pending 行，
--     worker 按投递时刻的端点数据签名发送并回写结果（改 URL / 停用即时生效）。

-- 1. webhook_endpoints —— 外部集成端点（白名单条目）
CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id            BIGSERIAL    PRIMARY KEY,
    -- 订阅的事件类型（如 order.paid / content.published）
    event_type    VARCHAR(128) NOT NULL,
    -- 目标 URL（仅 http/https；写入与投递时都过 DNS 解析后的内网 IP 检查）
    target_url    VARCHAR(1024) NOT NULL,
    -- HMAC-SHA256 签名密钥（加密存储）
    secret_cipher VARCHAR(512) NOT NULL,
    description   VARCHAR(512) NOT NULL DEFAULT '',
    -- 1 = 启用 / 0 = 停用
    status        SMALLINT     NOT NULL DEFAULT 1,
    created_at    BIGINT       NOT NULL,
    updated_at    BIGINT       NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_event_type ON webhook_endpoints (event_type);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_status ON webhook_endpoints (status);

-- 2. webhook_deliveries —— 投递日志（幂等守卫：仅 pending 会被 worker 处理）
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              BIGSERIAL    PRIMARY KEY,
    endpoint_id     BIGINT       NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type      VARCHAR(128) NOT NULL,
    -- 事件负载 JSON（入队时已限 64KiB）
    payload         TEXT         NOT NULL,
    -- pending / delivered / failed
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    attempts        INTEGER      NOT NULL DEFAULT 0,
    response_status INTEGER,
    last_error      VARCHAR(1024) NOT NULL DEFAULT '',
    created_at      BIGINT       NOT NULL,
    updated_at      BIGINT       NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status ON webhook_deliveries (status);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event_type ON webhook_deliveries (event_type);
