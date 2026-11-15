package migrations

import "sync"

// 537-ai-call-log 是纯 DDL，登记为迁移（原因同 511）。
func registerAICallLog() {
	registerAICallLogOnce.Do(registerAICallLogMigration)
}

var registerAICallLogOnce sync.Once

func registerAICallLogMigration() {
	register(Migration{
		Version:   "537-ai-call-log",
		TableName: "ai_call_log",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_call_log') IS NOT NULL AND (SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'ai_call_log' AND indexname IN ('idx_ai_call_log_session_time', 'idx_ai_call_log_user_time', 'idx_ai_call_log_time')) = 3 THEN 1 ELSE 0 END",
		SQL:       mustSQL("537_ai_call_log.sql"),
	})
}
