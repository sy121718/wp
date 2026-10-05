package migrations

import "sync"

// register_ai_usage_metrics_i18n.go — 会话页「验收数字」三张卡的词条（567）。
//
// 判据按本批自己的对象枚举：命中率与摘要占用两个代表 key，各自的中英两条都要在
// （7 键 × 2 = 14 条；代表 key 语言齐全即认完成）。
func registerAIUsageMetricsI18n() {
	registerAIUsageMetricsI18nOnce.Do(registerAIUsageMetricsI18nSeed)
}

var registerAIUsageMetricsI18nOnce sync.Once

func registerAIUsageMetricsI18nSeed() {
	registerSeed(Seed{
		Version:   "567-ai-usage-metrics-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.stat.hitRate', 'admin.ai.session.stat.compactCost') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("567_ai_usage_metrics_i18n.sql"),
	})
}
