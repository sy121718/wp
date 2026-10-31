package migrations

import "sync"

func registerProductEditI18n() {
	registerProductEditI18nOnce.Do(registerProductEditI18nSeed)
}

var registerProductEditI18nOnce sync.Once

func registerProductEditI18nSeed() {

	registerSeed(Seed{
		Version:   "314-i18n-seed-product-edit",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.products.row.edit','admin.products.row.preview','product.msg.saved')",
		SQL: mustSQL("314_i18n_seed_product_edit.sql"),
	})
}
