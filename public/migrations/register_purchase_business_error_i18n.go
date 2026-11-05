package migrations

func init() {
	registerSeed(Seed{
		Version:   "727-purchase-business-error-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('ErrReceiptOrderDone', 'zh-CN'), ('ErrReceiptOrderDone', 'en-US'),
  ('ErrPurchaseLinesRequired', 'zh-CN'), ('ErrPurchaseLinesRequired', 'en-US'),
  ('ErrStockSKURequired', 'zh-CN'), ('ErrStockSKURequired', 'en-US'),
  ('ErrReceiptLinesRequired', 'zh-CN'), ('ErrReceiptLinesRequired', 'en-US'),
  ('ErrProductionVariantRequired', 'zh-CN'), ('ErrProductionVariantRequired', 'en-US'))`,
		SQL: mustSQL("727_purchase_business_error_i18n.sql"),
	})
}
