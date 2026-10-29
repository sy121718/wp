package migrations

// 逐条枚举本批 5 个键的中英文，缺任意一对都会补齐。
func init() {
	registerSeed(Seed{Version: "695-product-atomic-writes-i18n", TableName: "sys_i18n", SQL: mustSQL("695_product_atomic_writes_i18n.sql"), ConditionSQL: `SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('admin.product_pricing.apply.atomicHint', 'zh-CN'), ('admin.product_pricing.apply.atomicHint', 'en-US'), ('ErrPricingCostMissing', 'zh-CN'), ('ErrPricingCostMissing', 'en-US'), ('ErrPricingOutOfRange', 'zh-CN'), ('ErrPricingOutOfRange', 'en-US'), ('ErrPricingTargetChanged', 'zh-CN'), ('ErrPricingTargetChanged', 'en-US'), ('ErrVariantListDuplicated', 'zh-CN'), ('ErrVariantListDuplicated', 'en-US'))`})
}
