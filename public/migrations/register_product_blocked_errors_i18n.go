package migrations

func init() {
	registerSeed(Seed{
		Version:   "704-product-blocked-errors-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("718q_current_704_product_blocked_errors_i18n.sql"),
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (
			('pBulkReject', 'zh-CN'), ('pBulkReject', 'en-US'),
			('pVarStockDup', 'zh-CN'), ('pVarStockDup', 'en-US'))`,
	})
}
