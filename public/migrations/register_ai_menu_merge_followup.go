package migrations

import "sync"

// register_ai_menu_merge_followup.go — 532 的收口（534）。
//
// 判据枚举本批自己的三件事（上界封闭）：① 515 插回的 /admin/ai/providers 已无活行；
// ② admin.ai.lead 的中文已是新文案；③ 英文也是。三件都成立才跳过。
//
// 只判其中一件时另外两件漏做也永远补不上 —— 而这两件的失效方式都是「静默」：
// 菜单多一条不影响功能，文案旧一条没人报错。
func registerAIMenuMergeFollowup() {
	registerAIMenuMergeFollowupOnce.Do(registerAIMenuMergeFollowupSeed)
}

// registerAIMenuMergeFollowupOnce 让重复调用成为空操作。
var registerAIMenuMergeFollowupOnce sync.Once

// registerAIMenuMergeFollowupSeed 注册 534（真正干活的那一半）。
func registerAIMenuMergeFollowupSeed() {
	registerSeed(Seed{
		Version:   "534-ai-menu-merge-followup",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/providers' AND deleted_at IS NULL) = 0 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'admin.ai.lead' " +
			"AND lang = 'zh-CN' AND item_value LIKE '两个标签：%') = 1 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'admin.ai.lead' " +
			"AND lang = 'en-US' AND item_value LIKE 'Two tabs:%') = 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("534_ai_menu_merge_followup.sql"),
	})
}
