package migrations

func init() {
	registerSeed(Seed{
		Version:   "465-shipping-settings-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'ErrShippingBaseFeeInvalid', 'ErrShippingFreeThresholdInvalid', " +
			"'admin.settings.section.shipping', 'admin.settings.field.shipping_base_fee', " +
			"'admin.settings.ph.shipping_base_fee', 'admin.settings.hint.shipping', " +
			"'admin.settings.field.shipping_free_threshold', " +
			"'admin.settings.ph.shipping_free_threshold', " +
			"'admin.settings.hint.shipping_threshold'" +
			")",
		SQL: mustSQL("465_shipping_settings_i18n.sql"),
	})
}
