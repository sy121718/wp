package aienums

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
