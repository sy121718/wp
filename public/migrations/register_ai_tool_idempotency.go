package migrations

import "sync"

// register_ai_tool_idempotency.go — 写工具的幂等台账（569）。
//
// 用 register（Migration）而不是 registerSeed：本批只有建表，**没有依赖任何 seed
// 建出来的对象**（不像 566 要 ALTER 537 建的表），所以不受「Migrations 先跑、
// Seeds 后跑」的顺序影响。反过来，写成 Seed 会让它在每次启动时多走一次
// ConditionSQL 查询，而这条判据（表在不在）对已建好的库毫无判别力。
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
