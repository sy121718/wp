package migrations

import "sync"

// register_ai_fab_i18n.go — 全局 AI 悬浮球的词条（568）。
//
// 判据按本批自己的对象枚举（上界封闭）：**只取模板侧的两个代表 key**，
// 各自的中英两条都要在（12 键 × 2 = 24 条；代表 key 语言齐全即认完成）。
//
// 代表 key 刻意**只取模板侧的**（admin.ai.fab.*）：那几条的 key 由模板的 t(...) 决定，
// 与本批 SQL 一一对应；而 handler 侧的 `ai.fab.*` 由 enums 常量给出，
// 将来只改那一侧文案就会让判据先失效 → 种子重跑 → 撞唯一键，
// 而失败的形态是「幂等性测试红」，与改动本身看起来毫无关系。
//
// **本批实测踩过一次**：判据里原本写的是 `admin.ai.fab.noModel`，而这个 key 属于
// handler 侧、命名空间是 `ai.fab.noModel` —— 判据里数到 <4 条 → 永远不满足 →
// 每次启动重插 24 条 → uk_sys_i18n_key_lang 冲突。教训：**判据里的 key 必须是
// 本批 SQL 里逐字存在的那几个**，「我以为它会叫这个名字」不算。
func registerAIFabI18n() {
	registerAIFabI18nOnce.Do(registerAIFabI18nSeed)
}

var registerAIFabI18nOnce sync.Once

func registerAIFabI18nSeed() {
	registerSeed(Seed{
		Version:   "568-ai-fab-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.fab.title', 'admin.ai.fab.toggle') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("568_ai_fab_i18n.sql"),
	})
}
