package migrations

import "sync"

// register_ai_session_trend_other_i18n.go — 折线图图例「其他」的词条（531）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：本批 1 个 key × 2 语言 = 2 条元组，
// 两个语言都在才视为完成 —— 只判一个语言时，另一条漏插了也永远补不上。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAISessionTrendOtherI18n() {
	registerAISessionTrendOtherI18nOnce.Do(registerAISessionTrendOtherI18nSeed)
}

// registerAISessionTrendOtherI18nOnce 让重复调用成为空操作。
var registerAISessionTrendOtherI18nOnce sync.Once

// registerAISessionTrendOtherI18nSeed 注册 531（真正干活的那一半）。
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
