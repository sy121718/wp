package migrations

func init() {
	registerSeed(Seed{
		Version:   "734-order-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 34 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('ordSaved', 'zh-CN'), ('ordSaved', 'en-US'),
  ('ordStChanged', 'zh-CN'), ('ordStChanged', 'en-US'),
  ('ordCancelled', 'zh-CN'), ('ordCancelled', 'en-US'),
  ('ordRefunded', 'zh-CN'), ('ordRefunded', 'en-US'),
  ('ordNoteSaved', 'zh-CN'), ('ordNoteSaved', 'en-US'),
  ('ordCpCreated', 'zh-CN'), ('ordCpCreated', 'en-US'),
  ('ordCpUpdated', 'zh-CN'), ('ordCpUpdated', 'en-US'),
  ('ordCpDeleted', 'zh-CN'), ('ordCpDeleted', 'en-US'),
  ('ordShipped', 'zh-CN'), ('ordShipped', 'en-US'),
  ('ordCarrierSaved', 'zh-CN'), ('ordCarrierSaved', 'en-US'),
  ('ordRetApproved', 'zh-CN'), ('ordRetApproved', 'en-US'),
  ('ordRetRejected', 'zh-CN'), ('ordRetRejected', 'en-US'),
  ('ordRetReceived', 'zh-CN'), ('ordRetReceived', 'en-US'),
  ('ordBulkNone', 'zh-CN'), ('ordBulkNone', 'en-US'),
  ('ordBulkDone', 'zh-CN'), ('ordBulkDone', 'en-US'),
  ('ordBulkPart', 'zh-CN'), ('ordBulkPart', 'en-US'),
  ('ordBulkSkip', 'zh-CN'), ('ordBulkSkip', 'en-US'))`,
		SQL: mustSQL("734_order_write_result_i18n.sql"),
	})
}
