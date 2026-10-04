-- 539 · AI 会话「工具调用」的词条。
--
-- 背景：本批给会话页的「发消息」接上工具调用（agent loop）—— 模型可以要求执行
--   orders_summary 这类只读查询，结果作为 kind=tool 的事件落进事件日志。
--   这三条 key 都出现在用户看得见的地方：轮次超限是发消息的失败回执（走 pkg/response 翻译），
--   另两条会被写进 tool 事件的正文（会话页逐条显示），所以三条都要有词条。
--   判据见 scripts/check-i18n-keys-seeded.sh（只认 INSERT 元组，注释与 ConditionSQL 不算）。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的代表 key（register_ai_session_tools_i18n.go），
--   不用 LIKE 前缀 / 全库计数（存量库永远满足 → 补词条的那条迁移永远不会执行，迁移 494 记过这个坑）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('ai.err.sessionToolRoundsExceeded', 'zh-CN', '模型连续调用工具次数过多，已停止，请重试或换个说法', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionToolRoundsExceeded', 1, now(), now()),
('ai.err.sessionToolRoundsExceeded', 'en-US', 'The model called tools too many times in a row and was stopped; please retry or rephrase', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionToolRoundsExceeded', 1, now(), now()),
('ai.err.toolForbidden', 'zh-CN', '没有权限执行该操作', 403, 'ai', 'enums/ai_session_msg.go: ErrToolForbidden', 1, now(), now()),
('ai.err.toolForbidden', 'en-US', 'You do not have permission to do that', 403, 'ai', 'enums/ai_session_msg.go: ErrToolForbidden', 1, now(), now()),
('ai.err.toolRunFailed', 'zh-CN', '工具执行失败', 500, 'ai', 'enums/ai_session_msg.go: ErrToolRunFailed', 1, now(), now()),
('ai.err.toolRunFailed', 'en-US', 'The tool failed to run', 500, 'ai', 'enums/ai_session_msg.go: ErrToolRunFailed', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
