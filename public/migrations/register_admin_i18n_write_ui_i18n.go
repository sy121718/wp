package migrations

import "sync"

func registerAdminI18nWriteUII18n() {
	registerAdminI18nWriteUII18nOnce.Do(registerAdminI18nWriteUII18nSeed)
}

var registerAdminI18nWriteUII18nOnce sync.Once

func registerAdminI18nWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "595-admin-i18n-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.i18nWrite.bulkResult.allDeleted', 'admin.i18nWrite.bulkResult.allSkipped', " +
			"'admin.i18nWrite.bulkResult.partial', 'admin.i18nWrite.refresh', 'admin.i18nWrite.noJs')",
		SQL: mustSQL("595_admin_i18n_write_ui_i18n.sql"),
	})
}
