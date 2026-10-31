package migrations

import "sync"

func registerDashboardNewCustomersKPI18n() {
	registerDashboardNewCustomersKPI18nOnce.Do(registerDashboardNewCustomersKPI18nSeed)
}

var registerDashboardNewCustomersKPI18nOnce sync.Once

func registerDashboardNewCustomersKPI18nSeed() {
	registerSeed(Seed{
		Version:   "557-dashboard-new-customers-kpi-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.newCustomers') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("557_dashboard_new_customers_kpi_i18n.sql"),
	})
}
