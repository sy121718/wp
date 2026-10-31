package migrations

func registerCurrency() {

	register(Migration{
		Version:   "633-currency-rates",
		TableName: "currency_rates",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"UNION ALL " +
			"SELECT 1 FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = 'orders' AND column_name = 'fx_rate'" +
			") AS probe",
		SQL: mustSQL("633_currency_rates.sql"),
	})

	registerSeed(Seed{
		Version:   "633b-currency-converted-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 14 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.order.sales.converted.title', 'admin.order.sales.converted.hint', 'admin.order.sales.converted.sales', " +
			"'admin.order.sales.converted.profit', 'admin.order.sales.converted.orders', 'admin.order.sales.converted.unconverted', " +
			"'admin.order.sales.converted.unconvertedNote')",
		SQL: mustSQL("633b_currency_converted_i18n.sql"),
	})
}
