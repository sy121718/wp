package migrations

import "sync"

// register_menu_component_retire.go — component（组件路径）字段退役收尾（587）。
//
// 门槛探测「有没有跑过」，三条都算跑过（任一成立即跳过）：
//  1. 列已经不存在 —— 将来真删了列，本条的 COMMENT ON 会撞不存在的列，必须跳过；
//  2. 列注释已经打上 —— 正常路径；
//  3. 三条词条已经没了 —— 业务角色（非 sys_menus 属主）执行时 COMMENT 会被 DO 块吞掉，
//     注释永远打不上；若只看注释就会每次启动重跑一遍。DELETE 是普通 DML，业务角色能成功，
//     所以「词条已删」同样证明本批做过事。
// 不能只看 col_description：它对「列不存在」和「列在但没注释」都返回 NULL，两种情况必须分开。
// （migrator.go 的 applySeed：ConditionSQL 返回 > 0 则跳过；TableName 只进日志，不参与判定。）
func registerMenuComponentRetire() {
	registerMenuComponentRetireOnce.Do(registerMenuComponentRetireSeed)
}

var registerMenuComponentRetireOnce sync.Once

func registerMenuComponentRetireSeed() {
	registerSeed(Seed{
		Version:   "587-menu-component-retire",
		TableName: "sys_menus",
		SQL:       mustSQL("587_menu_component_retire.sql"),
		ConditionSQL: "SELECT CASE " +
			"WHEN NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'sys_menus'::regclass " +
			"AND attname = 'component' AND NOT attisdropped) THEN 1 " +
			"WHEN col_description('sys_menus'::regclass, (SELECT attnum FROM pg_attribute " +
			"WHERE attrelid = 'sys_menus'::regclass AND attname = 'component')) IS NOT NULL THEN 1 " +
			"WHEN NOT EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN " +
			"('ErrComponentRequired', 'ErrComponentNotAllowed', 'ErrComponentInvalid')) THEN 1 " +
			"ELSE 0 END",
	})
}
