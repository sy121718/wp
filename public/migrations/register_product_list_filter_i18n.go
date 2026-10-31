package migrations

import "sync"

func registerProductListFilterI18n() {
	registerProductListFilterI18nOnce.Do(registerProductListFilterI18nSeed)
}

var registerProductListFilterI18nOnce sync.Once

func registerProductListFilterI18nSeed() {

	registerSeed(Seed{
		Version:   "411-i18n-product-list-filter",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.common.filter.submit','admin.common.filter.reset'," +
			"'admin.common.filter.optionAll','admin.common.filter.emptyHint'," +
			"'admin.product_attributes.filter.phKeyword','admin.product_attributes.list.emptyFilteredTitle'," +
			"'admin.product_categories.filter.phKeyword','admin.product_categories.empty.filteredTitle'," +
			"'admin.product_brands.filter.phKeyword','admin.product_brands.empty.filteredTitle'," +
			"'admin.product_tags.filter.phKeyword','admin.product_tags.list.emptyFilteredTitle')",
		SQL: mustSQL("411_i18n_product_list_filter.sql"),
	})
}

func init() { registerProductListFilterI18n() }
