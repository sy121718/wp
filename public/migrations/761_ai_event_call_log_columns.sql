-- 761 · ai_event / ai_call_log 的观测列由 seed 收进迁移。
--
-- 529 / 535 / 566 当初登记成 Seed，但三份 SQL 全是幂等 DDL、没有一行数据：
-- 列与索引只存在于「跑过 seed」的库，只跑迁移的结构基线不含它们
-- （实测净差 5 列 + 2 索引）。结构归迁移、seed 只放数据，故收进本条迁移，
-- 三份 seed 随之退役；存量库由 CheckSQL（五列齐全即视为已应用）自动跳过。
--
-- 幂等：ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / COMMENT ON。

-- 529 · 这条事件是哪个供应商 / 哪个模型产生的。
-- 默认 '' 表示「未记录」：历史事件补不回来，展示时必须与真实值分开，
-- 不能让空串悄悄并进某个供应商的总量。
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS provider_key VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS model_id VARCHAR(120) NOT NULL DEFAULT '';
COMMENT ON COLUMN ai_event.provider_key IS '产生本条事件的供应商标识；空串 = 未记录（529 之前写入的历史事件）';
COMMENT ON COLUMN ai_event.model_id IS '产生本条事件的模型标识；空串 = 未记录（529 之前写入的历史事件）';
-- 按 (会话, 供应商, 模型) 聚合是这两列存在的理由，索引跟着聚合键走。
CREATE INDEX IF NOT EXISTS idx_ai_event_session_model ON ai_event (session_id, provider_key, model_id);

-- 535 · 这条事件是哪个后台账号写的（会话头 create_by 只记「谁开的会话」）。
-- 0 表示未记录，统计与展示要单独成组。
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS user_id BIGINT NOT NULL DEFAULT 0;
COMMENT ON COLUMN ai_event.user_id IS '写入这条事件的后台账号 id；0 = 未记录（535 之前写入的历史事件）';
-- 审计主路径是「某个人在某段时间里写了什么」，按 (user_id, create_time) 走。
CREATE INDEX IF NOT EXISTS idx_ai_event_user_time ON ai_event (user_id, create_time DESC);

-- 566 · 缓存命中 token 两列（判断稳定前缀纪律有没有生效的唯一观测）。
-- 两列而不是一列：cached_tokens 是上游报的命中数，cached_reported 是「上游到底报没报」。
-- 「没报」与「报了 0」必须分开：把没报的当 0 会拉低命中率，而失真方向恰好是「看起来更差」。
-- 只记上游上报值，不做本地估算补齐。
ALTER TABLE ai_call_log
    ADD COLUMN IF NOT EXISTS cached_tokens   BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_reported BOOLEAN NOT NULL DEFAULT FALSE;
COMMENT ON COLUMN ai_call_log.cached_tokens IS '输入里命中上游前缀缓存的 token 数（上游上报值，未报为 0）';
COMMENT ON COLUMN ai_call_log.cached_reported IS '上游是否报告过缓存命中数（false 时 cached_tokens 的 0 无意义）';
