package migrations

import "sync"

// register_ai_tool_call_log.go — 模型工具调用流水表（541）。
//
// 判据只枚举本批自己的对象（上界封闭）：本批只有 ai_tool_call_log 这一张表，
// 表在即完成。用 to_regclass 而不是「查 information_schema 计数」——
// 表名带 schema 前缀时计数容易写成「全库有多少张 ai_ 开头的表」，那是另一个判据（见 AGENTS.md）。
func registerAIToolCallLog() {
	registerAIToolCallLogOnce.Do(registerAIToolCallLogSeed)
}

// registerAIToolCallLogOnce 让重复调用成为空操作。
var registerAIToolCallLogOnce sync.Once

// registerAIToolCallLogSeed 注册 541（真正干活的那一半）。
func registerAIToolCallLogSeed() {
	registerSeed(Seed{
		Version:      "541-ai-tool-call-log",
		TableName:    "ai_tool_call_log",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_tool_call_log') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("541_ai_tool_call_log.sql"),
	})
}
