package migrations

// register_ai_tool_idempotency_subject.go — 678：写工具幂等台账加调用者维度（B02 / SEC-05）。
//
// 判据只查系统目录（pg_attribute / pg_constraint），不显式引用 actor 列本身：
// 迁移器先跑 CheckSQL 再决定是否执行，而 PG 在解析阶段就会对不存在的列报错
// （637/640/642/651/652/676 都踩过）。
//
// 三样都要判：只判列会让"列加了、唯一约束还是旧的"半成品库被静默跳过，
// 而旧约束恰好挡着跨主体各存一行 —— 那正是本迁移要修的东西。

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
