package migrations

import "sync"

func registerAdminCustomerFilterI18n() {
	registerAdminCustomerFilterI18nOnce.Do(registerAdminCustomerFilterI18nSeed)
}

var registerAdminCustomerFilterI18nOnce sync.Once

func registerAdminCustomerFilterI18nSeed() {

	registerSeed(Seed{
		Version:   "612-admin-customer-filter-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.customer.filter.rfm_unavailable', 'zh-CN'), ('admin.customer.filter.rfm_unavailable', 'en-US'), " +
			"('admin.customer.filter.segment_unavailable', 'zh-CN'), ('admin.customer.filter.segment_unavailable', 'en-US'), " +
			"('admin.customer.filter.tier_truncated', 'zh-CN'), ('admin.customer.filter.tier_truncated', 'en-US'), " +
			"('admin.customer.filter.tier_unavailable', 'zh-CN'), ('admin.customer.filter.tier_unavailable', 'en-US'), " +
			"('admin.customer.rfm.unavailable', 'zh-CN'), ('admin.customer.rfm.unavailable', 'en-US'), " +
			"('admin.products.rating.invalid', 'zh-CN'), ('admin.products.rating.invalid', 'en-US'))",
		SQL: mustSQL("612_admin_customer_filter_labels_i18n.sql"),
	})
}
