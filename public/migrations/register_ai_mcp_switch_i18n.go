package migrations

import "sync"

// register_ai_mcp_switch_i18n.go — 对外接入点开关的词条（549）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑状态徽标与开前置确认两个 key，
// 各自的中英两条都必须在（8 键 × 2 语言 = 16 条元组，代表 key 语言齐全即认完成）。
func registerAIMcpSwitchI18n() {
	registerAIMcpSwitchI18nOnce.Do(registerAIMcpSwitchI18nSeed)
}

// registerAIMcpSwitchI18nOnce 让重复调用成为空操作。
var registerAIMcpSwitchI18nOnce sync.Once

// registerAIMcpSwitchI18nSeed 注册 549（真正干活的那一半）。
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
