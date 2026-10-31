package migrations

func registerCustomerCatalog() {
	registerSeed(customerCatalogSeed())
}

func customerCatalogSeed() Seed {
	return Seed{
		Version:   "555-customer-nav-directory",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE deleted_at IS NULL AND (" +
			"(type = 1 AND title = '订单') " +
			"OR (type = 1 AND title = '客户') " +
			"OR (type = 2 AND path = '/admin/customers' AND title = '客户列表') " +
			"OR (type = 2 AND path = '/admin/customers/overview')" +
			")",
		SQL: mustSQL("555_customer_nav_directory.sql"),
	}
}
