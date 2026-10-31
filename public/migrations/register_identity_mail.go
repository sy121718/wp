package migrations

func registerIdentityAndMail() {

	register(Migration{
		Version:   "123-user",
		TableName: "users",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("123_user.sql"),
	})

	register(Migration{
		Version:   "124-mail",
		TableName: "mail_accounts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("124_mail.sql"),
	})

	register(Migration{
		Version:   "125-mail-marketing",
		TableName: "mail_contacts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("125_mail_marketing.sql"),
	})

	registerSeed(Seed{
		Version:      "126-mail-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:account_list'",
		SQL:          mustSQL("126_mail.sql"),
	})

	register(Migration{
		Version:   "127-mail-log-links",
		TableName: "mail_logs",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'campaign_id') THEN 1 ELSE 0 END",
		SQL:       mustSQL("127_mail_log_links.sql"),
	})

	registerSeed(Seed{
		Version:      "128-mail-campaign",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:campaign_list'",
		SQL:          mustSQL("128_mail_campaign.sql"),
	})

	registerSeed(Seed{
		Version:      "129-mail-templates",
		TableName:    "mail_templates",
		ConditionSQL: "SELECT COUNT(*) FROM mail_templates WHERE template_key = 'register_verify'",
		SQL:          mustSQL("129_mail_templates.sql"),
	})

	register(Migration{
		Version:   "130-mail-template-vars",
		TableName: "mail_templates",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'variables' AND data_type = 'ARRAY') THEN 1 ELSE 0 END",
		SQL:       mustSQL("130_mail_template_variables.sql"),
	})

	register(Migration{
		Version:   "131-mail-automation",
		TableName: "mail_automations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?",
		SQL:       mustSQL("131_mail_automation.sql"),
	})

	registerSeed(Seed{
		Version:      "132-mail-automation",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_list'",
		SQL:          mustSQL("132_mail_automation.sql"),
	})

	registerSeed(Seed{
		Version:      "133-mail-automation-layout",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_layout'",
		SQL:          mustSQL("133_mail_automation_layout_perm.sql"),
	})

	register(Migration{
		Version:   "134-inventory-reference-fks",
		TableName: "inventory_stocks",

		CheckSQL: "SELECT CASE WHEN COUNT(DISTINCT c.conname) = 8 THEN 1 ELSE 0 END FROM pg_constraint c WHERE (CAST(? AS text) IS NOT NULL) AND c.conname IN (" +
			"'fk_inventory_stocks_product', 'fk_inventory_purchase_lines_product'," +
			"'fk_inventory_purchase_lines_variant', 'fk_inventory_receipt_items_product'," +
			"'fk_inventory_receipt_items_variant', 'fk_inventory_movements_product'," +
			"'fk_product_price_adjustment_items_product', 'fk_product_price_adjustment_items_variant')",
		SQL: mustSQL("134_inventory_reference_fks.sql"),
	})

	registerSeed(Seed{
		Version:      "276-mail-err-internal-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'mail.err.internal'",
		SQL:          mustSQL("276_mail_err_internal_i18n.sql"),
	})
}
