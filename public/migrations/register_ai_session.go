package migrations

import "sync"

// 512-ai-session 同时建 ai_session 与 ai_event 两张表，纯 DDL，登记为迁移（原因同 511）。
func registerAISession() {
	registerAISessionOnce.Do(registerAISessionMigration)
}

var registerAISessionOnce sync.Once

func registerAISessionMigration() {
	register(Migration{
		Version:   "512-ai-session",
		TableName: "ai_session",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_session') IS NOT NULL AND to_regclass('ai_event') IS NOT NULL AND (SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND ((tablename = 'ai_session' AND indexname = 'idx_ai_session_status_time') OR (tablename = 'ai_event' AND indexname = 'idx_ai_event_session_kind'))) = 2 THEN 1 ELSE 0 END",
		SQL:       mustSQL("512_ai_session.sql"),
	})
}
