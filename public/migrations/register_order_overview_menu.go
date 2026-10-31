package migrations

import "sync"

func registerOrderOverviewMenu() {
	registerOrderOverviewMenuOnce.Do(registerOrderOverviewMenuSeed)
}

var registerOrderOverviewMenuOnce sync.Once

func registerOrderOverviewMenuSeed() {
	registerSeed(Seed{
		Version:   "580-order-overview-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/orders/overview') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("580_order_overview_menu.sql"),
	})
}
