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
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('pStockLink'),
    ('pTagsNone'),
    ('pdHintAutoTags'),
    ('pdHintRating'),
    ('pdHintReadonly')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723ac_current_315_i18n_seed_product_readonly.sql"),
	})
}
