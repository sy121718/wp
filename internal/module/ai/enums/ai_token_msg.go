// ai_token_msg.go — 令牌域的中文兜底文案。
//
// 与会话层同一机制（init 追加进 FacingMessages）：兜底不是给正式页面看的，
// 而是「词条迁移没跑到 / 新库还没 seed」时不至于直接漏出 key 本身。
package aienums

// tokenFacingMessages 令牌域的中文兜底，由 init 追加进 FacingMessages。
var tokenFacingMessages = map[string]string{
	ErrTokenNotFound:      "令牌不存在或已被删除",
	ErrTokenInvalid:       "访问令牌无效",
	ErrTokenNameRequired:  "请填写令牌用途",
	ErrTokenScopeRequired: "请至少选择一个权限点",
	ErrTokenScopeUnknown:  "包含未知的权限点",
	ErrTokenExpiredInPast: "过期时间必须晚于当前时间",
}

func init() {
	for k, v := range tokenFacingMessages {
		FacingMessages[k] = v
	}
}
