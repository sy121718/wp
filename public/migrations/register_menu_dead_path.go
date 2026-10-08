package migrations

import "sync"

// register_menu_dead_path.go — 三条「能力节点」菜单清空 path（588）。
//
// 门槛判据刻意**枚举本批自己的对象**（上界封闭，见 AGENTS.md §数据库）：那三条 path
// 是否都已经不在 sys_menus 里了。偏差方向取「宁可重跑」—— 将来若有人把 path 写回去，
// 本批会再跑一次把它清掉，而不是静默跳过。
//
// 不能写成「全库还有多少条带 path 的隐藏菜单」：那会把别的批次的隐藏菜单算进来，
// 计数永远追不平、每次启动都重跑（076 的真实故障）。
func registerMenuDeadPath() {
	registerMenuDeadPathOnce.Do(registerMenuDeadPathSeed)
}

var registerMenuDeadPathOnce sync.Once

func registerMenuDeadPathSeed() {
	registerSeed(Seed{
		Version:   "588-menu-dead-path-clear",
		TableName: "sys_menus",
		SQL:       mustSQL("588_menu_dead_path_clear.sql"),
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE deleted_at IS NULL AND is_hidden = 1 AND type = 2 " +
			"AND path IN ('/project', '/artifact', '/publication')) = 0 THEN 1 ELSE 0 END",
	})
}
