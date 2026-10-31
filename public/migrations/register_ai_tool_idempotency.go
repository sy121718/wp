package migrations

import "sync"

func registerAIToolIdempotency() {
	registerAIToolIdempotencyOnce.Do(registerAIToolIdempotencyMigration)
}

var registerAIToolIdempotencyOnce sync.Once

func registerAIToolIdempotencyMigration() {
	register(Migration{
		Version:   "569-ai-tool-idempotency",
		TableName: "ai_tool_idempotency",
		SQL:       mustSQL("569_ai_tool_idempotency.sql"),
	})
}
