package migrations

import "sync"

var registerAdminSystemWriteUII18nOnce sync.Once

func registerAdminSystemWriteUII18n() {
	registerAdminSystemWriteUII18nOnce.Do(func() {
		registerSeed(Seed{
			Version:      "611-admin-system-write-ui-i18n",
			TableName:    "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('admin.system.write.noJs', 'zh-CN'), ('admin.system.write.noJs', 'en-US'))",
			SQL:          mustSQL("611_admin_system_write_ui_i18n.sql"),
		})
	})
}
