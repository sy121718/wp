package migrations

func init() {
	registerSeed(Seed{
		Version:   "739-missing-ui-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('admin.membership.tier.deleteConfirm', 'zh-CN'), ('admin.membership.tier.deleteConfirm', 'en-US'),
  ('admin.returns.ph.bulk_reject_remark', 'zh-CN'), ('admin.returns.ph.bulk_reject_remark', 'en-US'),
  ('http.error.conflict', 'zh-CN'), ('http.error.conflict', 'en-US'),
  ('http.error.title', 'zh-CN'), ('http.error.title', 'en-US'))`,
		SQL: mustSQL("739_missing_ui_keys_i18n.sql"),
	})
}
