package migrations

func init() {
	registerSeed(Seed{Version: "695-product-atomic-writes-i18n", TableName: "sys_i18n", SQL: mustSQL("723r_current_695_product_atomic_writes_i18n.sql"), ConditionSQL: `SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('prAtomicHint', 'zh-CN'), ('prAtomicHint', 'en-US'), ('pPriceCostMiss', 'zh-CN'), ('pPriceCostMiss', 'en-US'), ('pPriceRangeBad', 'zh-CN'), ('pPriceRangeBad', 'en-US'), ('pPriceChanged', 'zh-CN'), ('pPriceChanged', 'en-US'), ('pVarListDup', 'zh-CN'), ('pVarListDup', 'en-US'))`})
}
