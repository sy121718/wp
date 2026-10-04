-- 535 · ai_event 补记 user_id（这条事件是哪个后台账号写的）。
--
-- 背景：会话头上的 create_by 记的是「谁开的会话」；一条会话可以被多个账号续写，
--   审计要能追到**每一条**的发起人。调用方（SendMessage / 页面追加）本来就带着 user_id，
--   只是过去没有落到事件上。
--
-- 与 529 同理：服务层第一关卡已经要求 UserID > 0，这里补的是**归属字段**而不是准入。
--   0 表示未记录（535 之前写入的历史事件），统计与展示要单独成组。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + CREATE INDEX IF NOT EXISTS。
ALTER TABLE ai_event ADD COLUMN IF NOT EXISTS user_id BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN ai_event.user_id IS '写入这条事件的后台账号 id；0 = 未记录（535 之前的历史事件）';

-- 审计查询的主路径是「某个人在某段时间里写了什么」，按 (user_id, create_time) 走。
CREATE INDEX IF NOT EXISTS idx_ai_event_user_time ON ai_event (user_id, create_time DESC);
