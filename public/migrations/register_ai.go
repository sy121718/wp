package migrations

import "sync"

// register_ai.go — AI 供应商 / 模型配置表（511）。
func registerAIProvider() {
	registerAIProviderOnce.Do(registerAIProviderSeed)
}

// registerAIProviderOnce 让重复调用成为空操作。
var registerAIProviderOnce sync.Once

// registerAIProviderSeed 注册 511（真正干活的那一半）。
//
// 判定只枚举本批自己的对象（上界封闭）：本批只建 ai_provider 一张表、不 seed 词条，
// 所以门槛就是 to_regclass('ai_provider') —— 表存在即视为本批已完成，整条跳过。
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerAIProviderSeed() {
	registerSeed(Seed{
		Version:      "511-ai-provider",
		TableName:    "ai_provider",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_provider') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("511_ai_provider.sql"),
	})
}
