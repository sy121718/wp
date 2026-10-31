package migrations

import "sync"

func registerCustomerCohortI18n() {
	registerCustomerCohortI18nOnce.Do(registerCustomerCohortI18nSeed)
}

var registerCustomerCohortI18nOnce sync.Once

func registerCustomerCohortI18nSeed() {
	registerSeed(Seed{
		Version:   "562-customer-cohort-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.cohort.col.cohort', 'admin.customer.cohort.totalCustomers') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("562_customer_cohort_i18n.sql"),
	})
}
