package migrations

import "sync"

func registerAISessionTrendOtherI18n() {
	registerAISessionTrendOtherI18nOnce.Do(registerAISessionTrendOtherI18nSeed)
}

var registerAISessionTrendOtherI18nOnce sync.Once

func registerAISessionTrendOtherI18nSeed() {
	registerSeed(Seed{
		Version:   "531-ai-session-trend-other-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'admin.ai.session.trend.other' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("531_ai_session_trend_other_i18n.sql"),
	})
}
