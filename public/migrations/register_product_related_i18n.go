package migrations

import "sync"

func registerProductRelatedIDsI18n() {
	registerProductRelatedIDsI18nOnce.Do(registerProductRelatedIDsI18nSeed)
}

var registerProductRelatedIDsI18nOnce sync.Once

func registerProductRelatedIDsI18nSeed() {

	registerSeed(Seed{
		Version:   "304-i18n-seed-product-related-ids",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('pRelatedInvalid')",
		SQL: mustSQL("718f_current_304_i18n_seed_product_related_ids.sql"),
	})
}
