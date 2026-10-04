-- 536 · AI 调用第一关卡的文案词条（ai.err.userRequired）。
--
-- 背景：535 起 ai_event 记 user_id，service 层把「没有身份就不受理」做成**第一关卡**
--   （ErrUserRequired，取值是 i18n key，登记在 enums）。key 已进 FacingMessages 的中文兜底，
--   但**词条本身从没 seed 过** —— 中文界面看不出来（兜底逐字一致），切到 en-US 才会暴露成中文。
--   这类缺口只有 check-i18n-keys-seeded.sh 抓得到，i18n 覆盖率门禁是绿的。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
--   DO NOTHING 意味着**不会覆盖**旧词条：这条 key 是新的，不存在被更早批次 seed 过的可能。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('ai.err.userRequired', 'zh-CN', '请先登录后再使用 AI 功能', 200, 'admin', 'ai: 调用大模型的第一关卡（未登录 / 取不到 user_id）', 1, now(), now()),
('ai.err.userRequired', 'en-US', 'Please sign in before using AI features.', 200, 'admin', 'ai: AI calls require an authenticated user', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
