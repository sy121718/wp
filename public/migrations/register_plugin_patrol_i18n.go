package migrations

// register_plugin_patrol_i18n.go — 插件「产物对账巡检」的词条（迁移 279）。
//
// 单独成一个主题文件、而不是挤进 register_admin_i18n.go：后者的函数名与注释都写着
// 「admin 后台词条」，插件巡检属于插件模块（internal/module/plugin/**）的界面文案，
// 混进去会让「这批词条是谁的」在 review 时看不出来。
// 目前 init() 里的注册函数是按主题手工列出的（见 register.go），所以新增主题文件要在
// 那里加一行调用 —— 这是刻意的：注册顺序在 diff 里可见。
func registerPluginPatrolI18n() {
	// 279：插件产物对账巡检的页面文案（12 个 key × 中英 = 24 行）。
	//
	// 判定枚举本批**全部 12 个 key**（>=12），不用全表行数：用全库行数会被同期其它批次
	// 的行满足而静默跳过（本仓库踩过，理由见 226/277）。
	// ConditionSQL 里没有占位符 —— 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、
	// 每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "279-plugin-patrol-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.plugins.patrol.orphans_title', 'admin.plugins.patrol.orphans_desc', " +
			"'admin.plugins.patrol.col_tables', 'admin.plugins.patrol.col_hint', " +
			"'admin.plugins.patrol.hint_empty', 'admin.plugins.patrol.hint_filled', " +
			"'admin.plugins.patrol.missing_title', 'admin.plugins.patrol.missing_desc', " +
			"'admin.plugins.patrol.orphan_storage_title', 'admin.plugins.patrol.orphan_storage_desc', " +
			"'admin.plugins.patrol.missing_storage_title', 'admin.plugins.patrol.missing_storage_desc')",
		SQL: mustSQL("279_plugin_patrol_i18n.sql"),
	})
}
