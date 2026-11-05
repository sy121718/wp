package migrations

func init() {
	registerSeed(Seed{
		Version:   "741-navigation-source-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('navSrcNone', 'zh-CN'), ('navSrcNone', 'en-US'),
  ('navSrcSkip', 'zh-CN'), ('navSrcSkip', 'en-US'),
  ('navSrcDone', 'zh-CN'), ('navSrcDone', 'en-US'),
  ('navSrcPart', 'zh-CN'), ('navSrcPart', 'en-US'))`,
		SQL: mustSQL("741_navigation_source_result_i18n.sql"),
	})
}
