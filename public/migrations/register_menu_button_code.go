package migrations

import "sync"

// register_menu_button_code.go — 按钮码（type=3 节点的 title_key）数据准备（589）。
//
// 门槛判据刻意**枚举本批自己的对象类**（上界封闭，见 AGENTS.md §数据库）：
// 「还有没有 type=3 节点没填 title_key」。偏差方向取「宁可重跑」——
// 将来新增按钮节点忘了填码时，本批会再跑一次把它补上（三条 SQL 都幂等），
// 而不是静默留一个模板查不到的码（那会让按钮永远不显示，且没有任何报错）。
func registerMenuButtonCode() {
	registerMenuButtonCodeOnce.Do(registerMenuButtonCodeSeed)
}

var registerMenuButtonCodeOnce sync.Once

func registerMenuButtonCodeSeed() {
	registerSeed(Seed{
		Version:   "589-menu-button-code",
		TableName: "sys_menus",
		SQL:       mustSQL("589_menu_button_code.sql"),
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE deleted_at IS NULL AND type = 3 AND (title_key IS NULL OR title_key = '')) = 0 " +
			"THEN 1 ELSE 0 END",
	})
}
