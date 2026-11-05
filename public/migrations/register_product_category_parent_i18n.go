package migrations

import "sync"

func init() { registerProductCategoryParentI18n() }

func registerProductCategoryParentI18n() {
	registerProductCategoryParentI18nOnce.Do(registerProductCategoryParentI18nSeed)
}

var registerProductCategoryParentI18nOnce sync.Once

func registerProductCategoryParentI18nSeed() {
	registerSeed(Seed{
		Version:   "508-i18n-category-parent-out-of-scope",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('catParentScope')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("720f_current_508_product_category_parent_i18n.sql"),
	})
}
