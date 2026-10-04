-- 537 · ai_call_log：大模型调用流水（谁、用哪家模型、多久、成没成、报了多少 token）。
--
-- 背景：512 里写明「计费与调用日志不在本批范围」，529 / 535 只补了**事件**上的来源与归属。
--   本批补上真正的**调用**视角：一次出站调用一行。
--
-- 为什么单独一张表而不是并进 ai_event（三条各自独立成立）：
--   · ai_event 是 append-only 的**业务真源**（参与投影与计量）。观测数据混进去，
--     会让「日志写失败」升级成「对话失败」，也会让投影多出一类不该进上下文的行；
--   · 调用流水要记的耗时 / 上游用量 / 错误分类，在事件表上没有位置；
--   · 本表**零投影、零计量**：只被出站层追加、被排查时读，读写都简单。
--
-- 写入方式：出站层**协程异步写**（脱离请求 ctx + 独立超时 + panic 不外溢，
--   见 internal/module/ai/service/ai_call_log.go）。代价是进程在落库前退出会丢一条 ——
--   观测数据丢一条不影响业务正确性，用「不阻塞用户等回复」换的，接受。
--
-- 用量三列的口径：**只记上游上报的值**，不做本地估算补齐。
--   usage_reported = false 表示这家没报 usage，此时三列恒为 0，语义是「不知道」而不是
--   「用了 0 个 token」—— 统计与展示必须把两者分开，否则未上报的调用看起来像没消耗。
--
-- error_key 存的是 i18n key（与 enums 哨兵同源），**不放底层原文**：
--   上游报文可能回显密钥或内部标识，原文只进日志。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / 独立 COMMENT ON，
--   重复执行安全；判据见 register_ai_call_log.go（只判本批这一张表在不在）。

CREATE TABLE IF NOT EXISTS ai_call_log (
    id             BIGSERIAL    PRIMARY KEY,
    session_id     BIGINT       NOT NULL DEFAULT 0,
    user_id        BIGINT       NOT NULL DEFAULT 0,
    provider_key   VARCHAR(50)  NOT NULL DEFAULT '',
    model_id       VARCHAR(120) NOT NULL DEFAULT '',
    protocol       VARCHAR(50)  NOT NULL DEFAULT '',
    input_tokens   BIGINT       NOT NULL DEFAULT 0,
    output_tokens  BIGINT       NOT NULL DEFAULT 0,
    total_tokens   BIGINT       NOT NULL DEFAULT 0,
    usage_reported BOOLEAN      NOT NULL DEFAULT false,
    latency_ms     BIGINT       NOT NULL DEFAULT 0,
    status         VARCHAR(20)  NOT NULL DEFAULT '',
    error_key      VARCHAR(80)  NOT NULL DEFAULT '',
    create_time    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE ai_call_log IS '大模型调用流水：一次出站调用一行（旁路观测，不参与会话投影与计量）';
COMMENT ON COLUMN ai_call_log.session_id IS '这次调用属于哪条会话；0 = 未记录（对外接口没有会话概念）';
COMMENT ON COLUMN ai_call_log.user_id IS '发起这次调用的后台账号 id；0 = 未记录';
COMMENT ON COLUMN ai_call_log.provider_key IS '实际出站用的供应商标识（trim 后的值，不是请求原文）';
COMMENT ON COLUMN ai_call_log.model_id IS '实际出站用的模型标识';
COMMENT ON COLUMN ai_call_log.protocol IS '实际出站用的协议（取自供应商配置）';
COMMENT ON COLUMN ai_call_log.usage_reported IS '上游是否上报了用量；false 时三个 token 列恒为 0（「不知道」而不是「0 消耗」）';
COMMENT ON COLUMN ai_call_log.latency_ms IS '从发起到拿到响应的墙钟耗时（毫秒，含 SSRF 校验与响应解析）';
COMMENT ON COLUMN ai_call_log.status IS 'ok / error（取值见 aienums.CallStatus）';
COMMENT ON COLUMN ai_call_log.error_key IS '失败时的 i18n key（与 enums 哨兵同源）；不存底层原文';

-- 三条读路径各配一个索引（写入是异步追加、量小，索引开销可接受）：
-- 会话详情看这条会话的调用流水；「某个人调了多少次」；全局最近调用。
CREATE INDEX IF NOT EXISTS idx_ai_call_log_session_time ON ai_call_log (session_id, create_time DESC);
CREATE INDEX IF NOT EXISTS idx_ai_call_log_user_time ON ai_call_log (user_id, create_time DESC);
CREATE INDEX IF NOT EXISTS idx_ai_call_log_time ON ai_call_log (create_time DESC);
