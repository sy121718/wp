package migrations

import "sync"

// 541-ai-tool-call-log 是纯 DDL，登记为迁移（原因同 511）。
func registerAIToolCallLog() {
	registerAIToolCallLogOnce.Do(registerAIToolCallLogMigration)
}

var registerAIToolCallLogOnce sync.Once

func registerAIToolCallLogMigration() {
	register(Migration{
		Version:   "541-ai-tool-call-log",
		TableName: "ai_tool_call_log",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_tool_call_log') IS NOT NULL AND (SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'ai_tool_call_log' AND indexname IN ('idx_ai_tool_call_log_session', 'idx_ai_tool_call_log_user', 'idx_ai_tool_call_log_time', 'idx_ai_tool_call_log_tool_status')) = 4 THEN 1 ELSE 0 END",
		SQL:       mustSQL("541_ai_tool_call_log.sql"),
	})
}
