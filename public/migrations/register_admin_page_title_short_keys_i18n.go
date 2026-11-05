package migrations

func init() {
	registerSeed(Seed{
		Version:   "735-admin-page-title-short-keys",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('dataruleTitle', 'zh-CN'), ('dataruleTitle', 'en-US'),
  ('deptTitle', 'zh-CN'), ('deptTitle', 'en-US'),
  ('adminTitle', 'zh-CN'), ('adminTitle', 'en-US'),
  ('permTitle', 'zh-CN'), ('permTitle', 'en-US'),
  ('langUrlOffWarn', 'zh-CN'), ('langUrlOffWarn', 'en-US'))`,
		SQL: mustSQL("735_admin_page_title_short_keys_i18n.sql"),
	})
}
