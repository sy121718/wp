package migrations

func init() {
	registerSeed(Seed{
		Version:   "444-i18n-bundle-option-none",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.product_bundle.option.none' AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("444_i18n_bundle_option_none.sql"),
	})
}
