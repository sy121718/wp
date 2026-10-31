package migrations

import "sync"

func registerProductTagCrossProjectI18n() {
	registerProductTagCrossProjectI18nOnce.Do(registerProductTagCrossProjectI18nSeed)
}

var registerProductTagCrossProjectI18nOnce sync.Once

func registerProductTagCrossProjectI18nSeed() {

	registerSeed(Seed{
		Version:   "312-i18n-seed-product-tag-cross-project",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('ErrTagCrossProject')",
		SQL: mustSQL("312_i18n_seed_product_tag_cross_project.sql"),
	})
}
