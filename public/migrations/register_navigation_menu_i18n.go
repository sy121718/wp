package migrations

import "sync"

func registerNavigationMenuI18n() {
	registerNavigationMenuI18nOnce.Do(registerNavigationMenuI18nSeed)
}

var registerNavigationMenuI18nOnce sync.Once

func registerNavigationMenuI18nSeed() {

	registerSeed(Seed{
		Version:   "290-navigation-menu-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 19 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.navigations.kind.headerMobile', 'admin.navigations.kind.footerMobile', " +
			"'admin.navigations.panel.label', 'admin.navigations.panel.none', " +
			"'admin.navigations.panel.width', 'admin.navigations.panel.widthAuto', " +
			"'admin.navigations.panel.widthFull', 'admin.navigations.panel.save', " +
			"'admin.navigations.panel.create', 'admin.navigations.panel.preview', " +
			"'admin.navigations.panel.previewClose', 'admin.navigations.bulk_delete_confirm', " +
			"'admin.navigations.last_error', 'ErrStaleVersion', " +
			"'workbench.nav.new', 'workbench.nav.title', 'workbench.nav.path', " +
			"'workbench.nav.kind', 'workbench.nav.create')",
		SQL: mustSQL("290_navigation_menu_i18n.sql"),
	})
}
