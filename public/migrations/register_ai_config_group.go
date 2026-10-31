package migrations

import "sync"

func registerAIConfigGroup() {
	registerAIConfigGroupOnce.Do(registerAIConfigGroupSeed)
}

var registerAIConfigGroupOnce sync.Once

func registerAIConfigGroupSeed() {
	registerSeed(Seed{
		Version:      "548-ai-config-group",
		TableName:    "sys_config",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_config WHERE group_key = 'ai') >= 1 THEN 1 ELSE 0 END",
		SQL:          mustSQL("548_ai_config_group.sql"),
	})
}
