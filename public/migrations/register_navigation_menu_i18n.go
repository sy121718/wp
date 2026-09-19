package migrations

import "sync"

// register_navigation_menu_i18n.go — 导航菜单页与工作台检查器的补漏词条（迁移 290）。
//
// 为什么单独成一个主题文件：这一批跨两个模块的界面（navigation 的后台管理页 +
// workbench 的检查器面板），但改的是**同一件事** —— 模板里已有 t(key, 中文兜底)、
// sys_i18n 里却没有对应行的文案；混进 register_admin_i18n.go（那个文件的名字与注释都
// 写着「admin 后台词条」）会让「这批词条是谁的」在 review 时看不出来。
//
// 三组覆盖见 290 的 SQL 头注释：① 移动端位置名与悬浮面板区（迁移 285 的配套遗漏）；
// ② 乐观锁冲突（本批新增的 ErrStaleVersion）；③ 检查器的就地新建菜单项。
func registerNavigationMenuI18n() {
	registerNavigationMenuI18nOnce.Do(registerNavigationMenuI18nSeed)
}

// registerNavigationMenuI18nOnce 让「register.go 的显式一行」重复出现也安全
// （重复调用只是空操作，不会重复 registerSeed —— 重复注册同一 Version 会撞上
// TestSeedVersionsUnique）。
var registerNavigationMenuI18nOnce sync.Once

// registerNavigationMenuI18nSeed 注册 290 的 seed（真正干活的那一半）。
func registerNavigationMenuI18nSeed() {
	// 290：19 个 key × 中英 = 38 行。
	//
	// 判定枚举本批**全部 19 个 key**（>= 19），不用全库行数：用全库行数会被同期其它批次
	// 的行满足而静默跳过（本仓库踩过，理由见 226/277/283）。ConditionSQL 里没有占位符 ——
	// 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、每次启动重跑（178 踩过）。
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
