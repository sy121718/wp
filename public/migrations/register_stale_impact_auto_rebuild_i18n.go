package migrations

// register_stale_impact_auto_rebuild_i18n.go — 473：待重建影响面的说明文案随自动重建对齐。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 471/472/469 同形）。
//
// 与其它 i18n seed 不同的是：这里**更新既有值**（ON CONFLICT DO UPDATE）——
// 文案本身就错了（在教用户「去重建」，而系统已经自动重建），不是缺一条的问题。
// 门槛判据要求两种语言的新值都已落库，偏差方向选「宁可重跑」。
func init() {
	registerSeed(Seed{
		Version:   "473-stale-impact-auto-rebuild-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.blocks.impact.help' " +
			"AND (item_value LIKE '%自动重建%' OR item_value LIKE '%rebuilt automatically%')",
		SQL: mustSQL("473_stale_impact_auto_rebuild_i18n.sql"),
	})
}
