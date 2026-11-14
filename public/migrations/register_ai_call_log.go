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
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_call_log') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:       mustSQL("537_ai_call_log.sql"),
	})
}
