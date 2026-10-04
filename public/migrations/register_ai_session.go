package migrations

import "sync"

// register_ai_session.go — AI 会话与事件日志（512，见 docs/16-ai-session-and-cache.md）。
func registerAISession() {
	registerAISessionOnce.Do(registerAISessionSeed)
}

// registerAISessionOnce 让重复调用成为空操作。
var registerAISessionOnce sync.Once

// registerAISessionSeed 注册 512（真正干活的那一半）。
//
// 判定只枚举本批自己的对象（上界封闭）：本批建 ai_session **与** ai_event 两张表，
// 门槛要求**两张表都在**才视为整批完成（评审 11：只判 ai_session 时，若第一条 DDL 成功、
// 第二条失败，重跑会因「ai_session 已存在」整条跳过，ai_event 永远不会被建）。
// 两表是同一批产物，判据必须一起成立。
// 之所以仍带 TableName：它只进错误信息，真正的跳过判据是 ConditionSQL。
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerAISessionSeed() {
	registerSeed(Seed{
		Version:      "512-ai-session",
		TableName:    "ai_session",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_session') IS NOT NULL AND to_regclass('ai_event') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("512_ai_session.sql"),
	})
}
