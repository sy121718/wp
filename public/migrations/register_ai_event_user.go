package migrations

import "sync"

// register_ai_event_user.go — ai_event 补记 user_id（535）。
//
// 判据只枚举本批自己的对象（上界封闭）：ai_event.user_id 这一列存在即完成。
// 用 information_schema 而不是「表存在」—— 表早就在，判据必须落在**本批新增的那一列**上，
// 否则这条迁移永远跳过、列永远补不上。
func registerAIEventUser() {
	registerAIEventUserOnce.Do(registerAIEventUserSeed)
}

// registerAIEventUserOnce 让重复调用成为空操作。
var registerAIEventUserOnce sync.Once

// registerAIEventUserSeed 注册 535（真正干活的那一半）。
func registerAIEventUserSeed() {
	registerSeed(Seed{
		Version:   "535-ai-event-user",
		TableName: "ai_event",
		ConditionSQL: `SELECT CASE WHEN EXISTS (` +
			`SELECT 1 FROM information_schema.columns ` +
			`WHERE table_name = 'ai_event' AND column_name = 'user_id') ` +
			`THEN 1 ELSE 0 END`,
		SQL: mustSQL("535_ai_event_user.sql"),
	})
}
