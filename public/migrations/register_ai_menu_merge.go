package migrations

import "sync"

// register_ai_menu_merge.go — AI 两个菜单入口合并 + 词条（532）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：① 只剩一条指向 /admin/ai/sessions 的活菜单、
// 且它已改名；② 旧的「AI 会话」条目不再有活行；③ 两个 key 的中英都在。
// 三件事都要成立才跳过 —— 只判其中一件时，另外两件漏做也永远补不上。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerAIMenuMerge() {
	registerAIMenuMergeOnce.Do(registerAIMenuMergeSeed)
}

// registerAIMenuMergeOnce 让重复调用成为空操作。
var registerAIMenuMergeOnce sync.Once

// registerAIMenuMergeSeed 注册 532（真正干活的那一半）。
func registerAIMenuMergeSeed() {
	registerSeed(Seed{
		Version:   "532-ai-menu-merge",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/sessions' " +
			"AND title = '大模型管理' AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menus WHERE title = 'AI 会话' AND deleted_at IS NULL) = 0 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('admin.ai.menu.title', 'admin.ai.noAccess') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("532_ai_menu_merge.sql"),
	})
}
