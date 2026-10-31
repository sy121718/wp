package migrations

import "sync"

func registerAIMcpSwitchI18n() {
	registerAIMcpSwitchI18nOnce.Do(registerAIMcpSwitchI18nSeed)
}

var registerAIMcpSwitchI18nOnce sync.Once

func registerAIMcpSwitchI18nSeed() {
	registerSeed(Seed{
		Version:   "549-ai-mcp-switch-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.mcp.switch.on', 'admin.ai.mcp.switch.confirm') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("549_ai_mcp_switch_i18n.sql"),
	})
}
