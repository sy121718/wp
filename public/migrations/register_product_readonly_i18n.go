package migrations

import "sync"

func registerProductReadonlyI18n() {
	registerProductReadonlyI18nOnce.Do(registerProductReadonlyI18nSeed)
}

var registerProductReadonlyI18nOnce sync.Once

func registerProductReadonlyI18nSeed() {

	registerSeed(Seed{
		Version:   "315-i18n-seed-product-readonly",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.product_detail.hint.readonly','admin.products.tags.none','admin.products.col.stockLink')",
		SQL: mustSQL("315_i18n_seed_product_readonly.sql"),
	})
}
