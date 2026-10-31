package migrations

import "sync"

var registerAIToolIdempotencySubjectOnce sync.Once

func init() { registerAIToolIdempotencySubject() }

func registerAIToolIdempotencySubject() {
	registerAIToolIdempotencySubjectOnce.Do(func() {
		register(Migration{
			Version:   "678-ai-tool-idempotency-subject",
			TableName: "ai_tool_idempotency",
			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_attribute
				WHERE attrelid = to_regclass('ai_tool_idempotency')
				  AND attname = 'actor' AND NOT attisdropped
			) = 1 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('ai_tool_idempotency')
				  AND conname = 'uk_ai_tool_idempotency_actor'
			) = 1 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('ai_tool_idempotency')
				  AND conname = 'uk_ai_tool_idempotency'
			) = 0 THEN 1 ELSE 0 END`,
			SQL: mustSQL("678_ai_tool_idempotency_subject.sql"),
		})
	})
}
