package migrations

import "sync"

func registerProductPreviewOverrideI18n() {
	registerProductPreviewOverrideI18nOnce.Do(registerProductPreviewOverrideI18nSeed)
}

var registerProductPreviewOverrideI18nOnce sync.Once

func registerProductPreviewOverrideI18nSeed() {

	registerSeed(Seed{
		Version:   "316-i18n-override-product-preview",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.products.row.preview' " +
			"AND ((lang = 'zh-CN' AND item_value = '预览') OR (lang = 'en-US' AND item_value = 'Preview'))",
		SQL: mustSQL("316_i18n_override_product_preview.sql"),
	})
}
