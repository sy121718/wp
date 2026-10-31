package migrations

import "sync"

func registerDashboardKPIWordingI18n() {
	registerDashboardKPIWordingI18nOnce.Do(registerDashboardKPIWordingI18nSeed)
}

var registerDashboardKPIWordingI18nOnce sync.Once

func registerDashboardKPIWordingI18nSeed() {
	registerSeed(Seed{
		Version:   "577-dashboard-kpi-wording-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("577_dashboard_kpi_wording_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key = 'admin.dashboard.kpi.sales' AND item_value IN ('销售额', 'Sales'))",
	})
}
