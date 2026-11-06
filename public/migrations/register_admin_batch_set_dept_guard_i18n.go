package migrations

func init() {
	registerSeed(Seed{
		Version:   "749-admin-batch-set-dept-guard-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=4 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
    ('admin.err.adminSelectionEmpty','zh-CN'), ('admin.err.adminSelectionEmpty','en-US'),
    ('admin.err.adminBatchTooMany','zh-CN'), ('admin.err.adminBatchTooMany','en-US'))`,
		SQL: mustSQL("749_admin_batch_set_dept_guard_i18n.sql"),
	})
}
