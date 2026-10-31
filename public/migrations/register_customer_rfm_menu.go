package migrations

import "sync"

func registerCustomerRfmMenu() {
	registerCustomerRfmMenuOnce.Do(registerCustomerRfmMenuSeed)
}

var registerCustomerRfmMenuOnce sync.Once

func registerCustomerRfmMenuSeed() {
	registerSeed(Seed{
		Version:   "559-customer-rfm-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/customers/rfm') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("559_customer_rfm_menu.sql"),
	})
}
