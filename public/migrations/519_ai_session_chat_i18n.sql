-- 519 · AI 会话页「发消息」的词条。
--
-- 背景：本批把会话页从「只能看」做成「能对话」—— 详情区新增发消息表单
--   （provider / model / 消息 / 最大输出 token），服务端新增 5 条响应 key
--   （enums/ai_session_msg.go 的 MsgSessionSent 与 4 条 ai.err.sessionChat*）。
--   模板里的中文一律走 t()，故这里为每个 key 补齐中英两条。
--   判据见 scripts/check-i18n-keys-seeded.sh（只认 INSERT 元组，注释与 ConditionSQL 不算）。
--
-- 覆盖两块（同一张表、同一批，故合成一条迁移）：
--   1. admin.ai.session.send.*  8 条：会话页发消息区的界面文案（admin/ai/sessions.html）。
--   2. ai.msg.sessionSent / ai.err.sessionChat*  5 条：服务端响应 key（enums/ai_session_msg.go）。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的代表 key（register_ai_session_chat_i18n.go），
--   不用 LIKE 前缀 / 全库计数（存量库永远满足 → 补词条的那条迁移永远不会执行，迁移 494 记过这个坑）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.session.send.title', 'zh-CN', '发消息', 200, 'admin', 'admin/ai/sessions.html: 发消息区标题', 1, now(), now()),
('admin.ai.session.send.title', 'en-US', 'Send a message', 200, 'admin', 'admin/ai/sessions.html: send section title', 1, now(), now()),
('admin.ai.session.send.noProvider', 'zh-CN', '还没有可用的供应商，先到「AI 供应商」页配置后再回来。', 200, 'admin', 'admin/ai/sessions.html: 无供应商提示', 1, now(), now()),
('admin.ai.session.send.noProvider', 'en-US', 'No provider is available yet — configure one on the AI providers page first.', 200, 'admin', 'admin/ai/sessions.html: no provider hint', 1, now(), now()),
('admin.ai.session.send.provider', 'zh-CN', '供应商', 200, 'admin', 'admin/ai/sessions.html: 供应商下拉标签', 1, now(), now()),
('admin.ai.session.send.provider', 'en-US', 'Provider', 200, 'admin', 'admin/ai/sessions.html: provider select label', 1, now(), now()),
('admin.ai.session.send.model', 'zh-CN', '模型', 200, 'admin', 'admin/ai/sessions.html: 模型输入标签', 1, now(), now()),
('admin.ai.session.send.model', 'en-US', 'Model', 200, 'admin', 'admin/ai/sessions.html: model input label', 1, now(), now()),
('admin.ai.session.send.input', 'zh-CN', '消息', 200, 'admin', 'admin/ai/sessions.html: 消息输入标签', 1, now(), now()),
('admin.ai.session.send.input', 'en-US', 'Message', 200, 'admin', 'admin/ai/sessions.html: message textarea label', 1, now(), now()),
('admin.ai.session.send.hint', 'zh-CN', '发送后先记下你的消息，再把当前上下文投影发给模型，回复追加进事件日志。', 200, 'admin', 'admin/ai/sessions.html: 发消息说明', 1, now(), now()),
('admin.ai.session.send.hint', 'en-US', 'Your message is stored first, then the current context projection is sent to the model and the reply is appended to the event log.', 200, 'admin', 'admin/ai/sessions.html: send hint', 1, now(), now()),
('admin.ai.session.send.maxTokens', 'zh-CN', '最大输出 token', 200, 'admin', 'admin/ai/sessions.html: 最大输出标签', 1, now(), now()),
('admin.ai.session.send.maxTokens', 'en-US', 'Max output tokens', 200, 'admin', 'admin/ai/sessions.html: max output label', 1, now(), now()),
('admin.ai.session.send.submit', 'zh-CN', '发送', 200, 'admin', 'admin/ai/sessions.html: 发送按钮', 1, now(), now()),
('admin.ai.session.send.submit', 'en-US', 'Send', 200, 'admin', 'admin/ai/sessions.html: send button', 1, now(), now()),

('ai.msg.sessionSent', 'zh-CN', '消息已发送，模型已回复', 200, 'ai', 'enums/ai_session_msg.go: MsgSessionSent', 1, now(), now()),
('ai.msg.sessionSent', 'en-US', 'Message sent and the model replied', 200, 'ai', 'enums/ai_session_msg.go: MsgSessionSent', 1, now(), now()),
('ai.err.sessionChatUnavailable', 'zh-CN', '当前未接入对话能力，无法在这里发消息', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatUnavailable', 1, now(), now()),
('ai.err.sessionChatUnavailable', 'en-US', 'Chat is not wired up, messages cannot be sent here', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatUnavailable', 1, now(), now()),
('ai.err.sessionChatInputRequired', 'zh-CN', '请输入消息内容', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatInputRequired', 1, now(), now()),
('ai.err.sessionChatInputRequired', 'en-US', 'Please enter a message', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatInputRequired', 1, now(), now()),
('ai.err.sessionChatModelRequired', 'zh-CN', '请选择要使用的供应商与模型', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatModelRequired', 1, now(), now()),
('ai.err.sessionChatModelRequired', 'en-US', 'Please choose a provider and a model', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatModelRequired', 1, now(), now()),
('ai.err.sessionChatEmptyReply', 'zh-CN', '模型没有返回内容，请重试', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatEmptyReply', 1, now(), now()),
('ai.err.sessionChatEmptyReply', 'en-US', 'The model returned no content, please retry', 400, 'ai', 'enums/ai_session_msg.go: ErrSessionChatEmptyReply', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
