package migrations

import "sync"

func registerProductTagHitsI18n() {
	registerProductTagHitsI18nOnce.Do(registerProductTagHitsI18nSeed)
}

var registerProductTagHitsI18nOnce sync.Once

func registerProductTagHitsI18nSeed() {

	registerSeed(Seed{
		Version:   "298-i18n-seed-product-tag-hits",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.product_tags.list.hitHint','admin.product_tags.hits.error')",
		SQL: mustSQL("298_i18n_seed_product_tag_hits.sql"),
	})
}
