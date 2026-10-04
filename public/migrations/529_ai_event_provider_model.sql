-- 529 · ai_event 补记「这条事件是哪个供应商 / 哪个模型产生的」。
--
-- 背景：用量页要按供应商与模型拆 token，而 ai_event 此前只记了**会话级**的
--   provider_key / model_id（一个会话中途可以换模型），所以只算得出「这个会话一共多少」，
--   拆不开。512 里明确写了「计费与调用日志不在本批范围」，本批仍不引入费用，只补这两个标识。
--
-- 默认 '' 表示「这一条没有记录」：历史事件补不回来，展示时必须把「未记录」与真实值分开，
--   不能让空串悄悄并进某个供应商的总量里（那样看起来像「某供应商消耗为 0」）。
--
-- 幂等：DDL 全部写成 IF NOT EXISTS / COMMENT ON，重复执行安全；
--   register 的 ConditionSQL 只决定「整批是否已视为完成」。
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS provider_key VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS model_id VARCHAR(120) NOT NULL DEFAULT '';
COMMENT ON COLUMN ai_event.provider_key IS '产生本条事件的供应商标识；空串 = 未记录（529 之前写入的历史事件）';
COMMENT ON COLUMN ai_event.model_id IS '产生本条事件的模型标识；空串 = 未记录（529 之前写入的历史事件）';
-- 按 (会话, 供应商, 模型) 聚合是这两列存在的理由，索引跟着聚合键走。
CREATE INDEX IF NOT EXISTS idx_ai_event_session_model ON ai_event (session_id, provider_key, model_id);
