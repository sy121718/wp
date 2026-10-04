package migrations

import "sync"

// register_customer_cohort_menu.go — 客户目录的「群组留存」菜单（561）。
//
// **必须是 registerSeed 不是 register**：Migration 的默认存在性检查只看 TableName
// 那张表在不在，sys_menus 早就在 → 会直接跳过；Seed.ConditionSQL 才问得出这一行在不在。
func registerCustomerCohortMenu() {
	registerCustomerCohortMenuOnce.Do(registerCustomerCohortMenuSeed)
}

// registerCustomerCohortMenuOnce 让重复调用成为空操作。
var registerCustomerCohortMenuOnce sync.Once

// registerCustomerCohortMenuSeed 注册 561（真正干活的那一半）。
func registerCustomerCohortMenuSeed() {
	registerSeed(Seed{
		Version:   "561-customer-cohort-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/customers/cohort') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("561_customer_cohort_menu.sql"),
	})
}
