package migrations

import "sync"

// register_ai_call_log_cached_tokens.go — ai_call_log 的缓存命中两列（566）。
//
// **必须是 Seed 而不是 Migration**：迁移与种子是两批执行的，**Migrations 先跑、Seeds 后跑**
// （AGENTS.md「数据库」一节记着这条，076/058 上各栽过一次）。而 537（建 ai_call_log 表）
// 是个 Seed —— 把本批写成 Migration 就会让 `ALTER TABLE ai_call_log` 跑在**建表之前**，
// 报 `relation "ai_call_log" does not exist`。实测：写成 Migration 时
// internal/web/shell 的导航菜单用例直接死在迁移阶段。
//
// 跨批依赖的判据要落在**结果**上：本批的判据是「这两列在不在」，
// 而不是「我执行的这条语句有没有报错」。
func registerAICallLogCachedTokens() {
	registerAICallLogCachedTokensOnce.Do(registerAICallLogCachedTokensSeed)
}

var registerAICallLogCachedTokensOnce sync.Once

func registerAICallLogCachedTokensSeed() {
	registerSeed(Seed{
		Version:   "566-ai-call-log-cached-tokens",
		TableName: "ai_call_log",
		// 两列都在才算完成。查 information_schema 而不是 to_regclass：
		// 本批改的是**列**，表一直在，用表存在当判据会永远判为已完成（这正是 566 第一次
		// 写成 Migration 时踩的同一个坑的另一面：判据必须与它判定的对象同粒度）。
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_name = 'ai_call_log' AND column_name IN ('cached_tokens', 'cached_reported')) = 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("566_ai_call_log_cached_tokens.sql"),
	})
}
