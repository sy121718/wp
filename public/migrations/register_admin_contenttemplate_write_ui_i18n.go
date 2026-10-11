package migrations

import "sync"

var registerAdminContentTemplateWriteUII18nOnce sync.Once

func registerAdminContentTemplateWriteUII18n() {
	registerAdminContentTemplateWriteUII18nOnce.Do(func() {
		registerSeed(Seed{Version: "609-admin-contenttemplate-write-ui-i18n", TableName: "sys_i18n", ConditionSQL: "SELECT CASE WHEN COUNT(*) = 14 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('admin.content.templates.bulk.noneSelected', 'zh-CN'), ('admin.content.templates.bulk.noneSelected', 'en-US'), ('admin.content.templates.bulk.allDeleted', 'zh-CN'), ('admin.content.templates.bulk.allDeleted', 'en-US'), ('admin.content.templates.bulk.allSkipped', 'zh-CN'), ('admin.content.templates.bulk.allSkipped', 'en-US'), ('admin.content.templates.bulk.partial', 'zh-CN'), ('admin.content.templates.bulk.partial', 'en-US'), ('admin.content.templates.bulk.refresh', 'zh-CN'), ('admin.content.templates.bulk.refresh', 'en-US'), ('admin.content.templates.write.noJs', 'zh-CN'), ('admin.content.templates.write.noJs', 'en-US'), ('admin.common.write.failed', 'zh-CN'), ('admin.common.write.failed', 'en-US'))", SQL: mustSQL("609_admin_contenttemplate_write_ui_i18n.sql")})
	})
}
