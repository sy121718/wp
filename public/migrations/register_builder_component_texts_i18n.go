package migrations

func init() {

	registerSeed(Seed{
		Version:   "456-i18n-builder-component-texts",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'site.component.addToCart.qtyAria'," +
			"'site.component.addToCart.notice.noProject'," +
			"'site.component.addToCart.notice.noVariant'," +
			"'site.component.cartIcon.loading'," +
			"'site.component.cartIcon.viewCart'," +
			"'site.component.cartIcon.notice') AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("456_i18n_builder_component_texts.sql"),
	})
}
