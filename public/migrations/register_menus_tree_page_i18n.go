package migrations

import "sync"

func registerMenusTreePageI18n() {
	registerMenusTreePageI18nOnce.Do(registerMenusTreePageI18nSeed)
}

var registerMenusTreePageI18nOnce sync.Once

func registerMenusTreePageI18nSeed() {
	registerSeed(Seed{
		Version:   "586-menus-tree-page-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("586_menus_tree_page_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.menus.tree.expandAll'",
	})
}
