package migrations

import "sync"

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
