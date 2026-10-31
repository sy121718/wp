package migrations

import "sync"

func registerCustomerCohortMenu() {
	registerCustomerCohortMenuOnce.Do(registerCustomerCohortMenuSeed)
}

var registerCustomerCohortMenuOnce sync.Once

func registerCustomerCohortMenuSeed() {
	registerSeed(Seed{
		Version:   "561-customer-cohort-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/customers/cohort') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("561_customer_cohort_menu.sql"),
	})
}
