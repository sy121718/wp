package migrations

import "sync"

// register_menus_tree_page_i18n.go — 菜单管理树状分页的词条（586）。
//
// 门槛取本批自己的代表 key：admin.menus.tree.expandAll 只可能由这一批种下，
// 它存在即本批已跑过。不要拿 admin.menus.filter_placeholder 计数 ——
// 那是 192 迁移种的，存量库永远满足，本条就永远不会执行。
func registerMenusTreePageI18n() {
	registerMenusTreePageI18nOnce.Do(registerMenusTreePageI18nSeed)
}

var registerMenusTreePageI18nOnce sync.Once

func registerMenusTreePageI18nSeed() {
	registerSeed(Seed{
		Version:   "586-menus-tree-page-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("586_menus_tree_page_i18n.sql"),
		// 1 个代表 key × 2 语言 = 2 行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.menus.tree.expandAll'",
	})
}
