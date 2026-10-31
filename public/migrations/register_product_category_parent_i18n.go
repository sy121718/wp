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
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 1 AND COUNT(*) >= 2 THEN 1 ELSE 0 END " +
			"FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN (" +
			"'admin.product_categories.parent.out_of_scope')",
		SQL: mustSQL("508_product_category_parent_i18n.sql"),
	})
}
