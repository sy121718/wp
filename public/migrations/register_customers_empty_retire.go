package migrations

import "sync"

func registerCustomersEmptyRetire() {
	registerCustomersEmptyRetireOnce.Do(registerCustomersEmptyRetireSeed)
}

var registerCustomersEmptyRetireOnce sync.Once

func registerCustomersEmptyRetireSeed() {

	registerSeed(Seed{
		Version:      "418-i18n-retire-customers-empty",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'admin.customers.empty'",
		SQL:          mustSQL("418_retire_customers_empty_i18n.sql"),
	})
}

func init() { registerCustomersEmptyRetire() }
