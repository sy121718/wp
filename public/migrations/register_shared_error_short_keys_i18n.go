package migrations

func init() {
	registerSeed(Seed{
		Version:   "738-shared-error-short-keys",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 12 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('sysErrBadParam', 'zh-CN'), ('sysErrBadParam', 'en-US'),
  ('sysErrNoPerm', 'zh-CN'), ('sysErrNoPerm', 'en-US'),
  ('sysErrTooLarge', 'zh-CN'), ('sysErrTooLarge', 'en-US'),
  ('uiOpSuccess', 'zh-CN'), ('uiOpSuccess', 'en-US'),
  ('uiDelConfirm', 'zh-CN'), ('uiDelConfirm', 'en-US'),
  ('brandNoneSel', 'zh-CN'), ('brandNoneSel', 'en-US'))`,
		SQL: mustSQL("738_shared_error_short_keys_i18n.sql"),
	})
}
