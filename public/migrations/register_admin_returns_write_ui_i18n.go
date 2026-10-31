package migrations

import "sync"

func registerAdminReturnsWriteUII18n() {
	registerAdminReturnsWriteUII18nOnce.Do(registerAdminReturnsWriteUII18nSeed)
}

var registerAdminReturnsWriteUII18nOnce sync.Once

func registerAdminReturnsWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "605-admin-returns-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.returns.bulk.noneSelected', 'admin.returns.bulk.refresh', " +
			"'admin.returns.write.noJs')",
		SQL: mustSQL("605_admin_returns_write_ui_i18n.sql"),
	})
}
