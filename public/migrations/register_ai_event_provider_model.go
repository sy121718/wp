package migrations

import "sync"

// register_ai_event_provider_model.go — ai_event 补记供应商/模型标识（529）。
//
// 判定只枚举本批自己的对象（上界封闭）：本批加 **两列**，门槛要求**两列都在**才视为完成 ——
// 只判其中一列时，若第一条 DDL 成功、第二条失败，重跑会因「第一列已存在」整条跳过，
// 第二列永远不会被加上（512 的 register 注释里记过同型故障）。
//
// TableName 只进错误信息，真正的跳过判据是 ConditionSQL。
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerAIEventProviderModel() {
	registerAIEventProviderModelOnce.Do(registerAIEventProviderModelSeed)
}

// registerAIEventProviderModelOnce 让重复调用成为空操作。
var registerAIEventProviderModelOnce sync.Once

// registerAIEventProviderModelSeed 注册 529（真正干活的那一半）。
func registerAIEventProviderModelSeed() {
	registerSeed(Seed{
		Version:   "529-ai-event-provider-model",
		TableName: "ai_event",
		ConditionSQL: "SELECT CASE WHEN " +
			"EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'ai_event' AND column_name = 'provider_key') " +
			"AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'ai_event' AND column_name = 'model_id') " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("529_ai_event_provider_model.sql"),
	})
}
