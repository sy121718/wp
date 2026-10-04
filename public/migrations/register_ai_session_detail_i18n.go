package migrations

import "sync"

// register_ai_session_detail_i18n.go — 「会话详情」区的词条（547）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑标题与空态两个 key，
// 各自的中英两条都必须在（7 键 × 2 语言 = 14 条元组，代表 key 语言齐全即认完成）。
func registerAISessionDetailI18n() {
	registerAISessionDetailI18nOnce.Do(registerAISessionDetailI18nSeed)
}

// registerAISessionDetailI18nOnce 让重复调用成为空操作。
var registerAISessionDetailI18nOnce sync.Once

// registerAISessionDetailI18nSeed 注册 547（真正干活的那一半）。
func registerAISessionDetailI18nSeed() {
	registerSeed(Seed{
		Version:   "547-ai-session-detail-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.detail.title', 'admin.ai.detail.noEvents') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("547_ai_session_detail_i18n.sql"),
	})
}
