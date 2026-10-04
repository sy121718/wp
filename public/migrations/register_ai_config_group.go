package migrations

import "sync"

// register_ai_config_group.go — AI 模块的全局开关组（548）。
//
// 判据只枚举本批自己的对象（上界封闭）：本批只有 sys_config 里的一行（group_key = 'ai'）。
// 用 group_key 判定而不是「本组里 mcp_enabled 是不是 false」：默认值将来可能改，
// 判定跟着值走会让迁移在改建之后反复重跑（幂等条件只该回答「这行在不在」）。
func registerAIConfigGroup() {
	registerAIConfigGroupOnce.Do(registerAIConfigGroupSeed)
}

// registerAIConfigGroupOnce 让重复调用成为空操作。
var registerAIConfigGroupOnce sync.Once

// registerAIConfigGroupSeed 注册 548（真正干活的那一半）。
func registerAIConfigGroupSeed() {
	registerSeed(Seed{
		Version:      "548-ai-config-group",
		TableName:    "sys_config",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_config WHERE group_key = 'ai') >= 1 THEN 1 ELSE 0 END",
		SQL:          mustSQL("548_ai_config_group.sql"),
	})
}
