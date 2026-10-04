package migrations

import "sync"

// register_ai_access_token.go — 对外访问令牌表（542）。
//
// 判据只枚举本批自己的对象（上界封闭）：本批只有 ai_access_token 这一张表，
// 表在即完成。用 to_regclass 而不是「查 information_schema 计数」——
// 表名带 schema 前缀时计数容易写成「全库有多少张 ai_ 开头的表」，那是另一个判据（见 AGENTS.md）。
func registerAIAccessToken() {
	registerAIAccessTokenOnce.Do(registerAIAccessTokenSeed)
}

// registerAIAccessTokenOnce 让重复调用成为空操作。
var registerAIAccessTokenOnce sync.Once

// registerAIAccessTokenSeed 注册 542（真正干活的那一半）。
func registerAIAccessTokenSeed() {
	registerSeed(Seed{
		Version:      "542-ai-access-token",
		TableName:    "ai_access_token",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_access_token') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("542_ai_access_token.sql"),
	})
}
