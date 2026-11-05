-- 711 · MCP（JSON-RPC）对外文案的两条界面词条
--
-- 为什么单独一批：`/mcp` 的 JSON-RPC 响应里 `error` / `content[].text` 是**给外部客户端看的文案**，
--   不是错误事实。旧代码把 ai.err.tokenInvalid / ai.err.toolRunFailed 直接塞进 message，
--   于是「错误事实 key」被当成了界面文案（internal/architecture/text_domain_test.go 判据：
--   消息 key 应取界面域）。这里给同一批文案一个界面域的 key，文案与 543 / 539 保持一致，
--   不让用户看到两套说法。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_ai_mcp_error_text_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.mcp.error.tokenInvalid', 'zh-CN', '访问令牌无效', 401, 'ai', 'inbound/http/ai_handle.go: MCP JSON-RPC error（与 ai.err.tokenInvalid 同文案，界面域 key）', 1, now(), now()),
('admin.ai.mcp.error.tokenInvalid', 'en-US', 'Invalid access token', 401, 'ai', 'inbound/http/ai_handle.go: MCP JSON-RPC error (same text as ai.err.tokenInvalid, UI-domain key)', 1, now(), now()),
('admin.ai.mcp.error.toolRunFailed', 'zh-CN', '工具执行失败', 500, 'ai', 'inbound/http/ai_handle.go: MCP JSON-RPC content text（与 ai.err.toolRunFailed 同文案，界面域 key）', 1, now(), now()),
('admin.ai.mcp.error.toolRunFailed', 'en-US', 'The tool failed to run', 500, 'ai', 'inbound/http/ai_handle.go: MCP JSON-RPC content text (same text as ai.err.toolRunFailed, UI-domain key)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
