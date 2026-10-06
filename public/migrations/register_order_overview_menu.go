package migrations

import "sync"

// register_order_overview_menu.go — 订单目录的「销售概览」菜单（580）。
//
// **必须是 registerSeed 不是 register**：Migration 的默认存在性检查只看 TableName
// 那张表在不在，sys_menus 早就在 → 会直接跳过；Seed.ConditionSQL 才问得出这一行在不在。
func registerOrderOverviewMenu() {
	registerOrderOverviewMenuOnce.Do(registerOrderOverviewMenuSeed)
}

// registerOrderOverviewMenuOnce 让重复调用成为空操作。
var registerOrderOverviewMenuOnce sync.Once

// registerOrderOverviewMenuSeed 注册 580（真正干活的那一半）。
//
// 判据按 **path** 而不是 title：title 是运营可以在后台改的中文名。
// 只数本批要插的那一行（上界封闭），不数「订单目录下的菜单总数」——
// 后者会随别的批次漂移，正是 058/076 两次真实故障的形状。
func registerOrderOverviewMenuSeed() {
	registerSeed(Seed{
		Version:   "580-order-overview-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/orders/overview') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("580_order_overview_menu.sql"),
	})
}
