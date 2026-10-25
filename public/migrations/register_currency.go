package migrations

// register_currency.go — 汇率表与订单汇率快照列（迁移 630，W5）。
//
// 单独一个主题文件而不是塞进 register_order.go：currency_rates 是**币种对**的事实，
// 不属任何工程、也不属订单域 —— 混进订单批次会让「这张表为什么存在」在 review 时
// 看不出来（与 register_*_i18n.go 单独成文件的理由同源）。
func registerCurrency() {
	// 630：currency_rates（覆盖式：同一币对只留最新一条）+ orders.fx_rate / fx_base_code。
	//
	// CheckSQL 按**本迁移自己的两张对象**判定（汇率表在 + orders.fx_rate 列在）：
	// 只查一张的话，中途失败会留下「表在、列不在」却被永久跳过的状态。
	register(Migration{
		Version:   "630-currency-rates",
		TableName: "currency_rates",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"UNION ALL " +
			"SELECT 1 FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = 'orders' AND column_name = 'fx_rate'" +
			") AS probe",
		SQL: mustSQL("630_currency_rates.sql"),
	})
}
