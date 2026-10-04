-- 543 · i18n 词条 seed（对外访问令牌 PAT）
--
-- 覆盖：6 个 key × 2 语言。全部是令牌域的错误文案（列表 / 签发 / 撤销接口的返回）。
--
-- http_code 的口径：
--   · 管理页操作失败（notFound / nameRequired / scopeRequired / scopeUnknown / expiredInPast）→ 400，
--     它们是「这次请求的参数不对」，不是身份问题；
--   · 对外调用令牌无效（tokenInvalid）→ 401，让外部 harness 能按状态码区分
--     「凭证不行（换令牌）」与「参数不行（改请求）」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_ai_token_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ai.err.tokenNotFound', 'zh-CN', '令牌不存在或已被删除', 400, 'ai', 'enums/ai_token_enums.go: ErrTokenNotFound', 1, now(), now()),
('ai.err.tokenNotFound', 'en-US', 'Token not found or already deleted', 400, 'ai', 'enums/ai_token_enums.go: ErrTokenNotFound', 1, now(), now()),
('ai.err.tokenInvalid', 'zh-CN', '访问令牌无效', 401, 'ai', 'enums/ai_token_enums.go: ErrTokenInvalid（不存在 / 已撤销 / 已过期统一说法）', 1, now(), now()),
('ai.err.tokenInvalid', 'en-US', 'Invalid access token', 401, 'ai', 'enums/ai_token_enums.go: ErrTokenInvalid', 1, now(), now()),
('ai.err.tokenNameRequired', 'zh-CN', '请填写令牌用途', 400, 'ai', 'service/ai_token_service.go: 创建时用途为空', 1, now(), now()),
('ai.err.tokenNameRequired', 'en-US', 'A purpose note is required', 400, 'ai', 'service/ai_token_service.go: empty name on create', 1, now(), now()),
('ai.err.tokenScopeRequired', 'zh-CN', '请至少选择一个权限点', 400, 'ai', 'service/ai_token_service.go: 创建时 scope 为空', 1, now(), now()),
('ai.err.tokenScopeRequired', 'en-US', 'Select at least one permission', 400, 'ai', 'service/ai_token_service.go: empty scopes on create', 1, now(), now()),
('ai.err.tokenScopeUnknown', 'zh-CN', '包含未知的权限点', 400, 'ai', 'service/ai_token_service.go: scope 含未登记的权限点', 1, now(), now()),
('ai.err.tokenScopeUnknown', 'en-US', 'Contains an unknown permission', 400, 'ai', 'service/ai_token_service.go: unknown scope entry', 1, now(), now()),
('ai.err.tokenExpiredInPast', 'zh-CN', '过期时间必须晚于当前时间', 400, 'ai', 'service/ai_token_service.go: 过期日已过去', 1, now(), now()),
('ai.err.tokenExpiredInPast', 'en-US', 'The expiry must be in the future', 400, 'ai', 'service/ai_token_service.go: expiry already past', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
