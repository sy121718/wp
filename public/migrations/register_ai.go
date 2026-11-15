package migrations

import "sync"

// 511-ai-provider 是纯 DDL（建表/索引/注释，无数据），登记为迁移而不是 seed：
// 结构必须由迁移阶段建，seed 只负责数据，否则结构基线（-skip-seeds）会缺这张表。
func registerAIProvider() {
	registerAIProviderOnce.Do(registerAIProviderMigration)
}

var registerAIProviderOnce sync.Once

func registerAIProviderMigration() {
	register(Migration{
		Version:   "511-ai-provider",
		TableName: "ai_provider",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_provider') IS NOT NULL AND (SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'ai_provider' AND indexname IN ('idx_ai_provider_status', 'idx_ai_provider_sort')) = 2 THEN 1 ELSE 0 END",
		SQL:       mustSQL("511_ai_provider.sql"),
	})
}
