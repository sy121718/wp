package migrations

import "sync"

func registerMailAutomationCanvasFieldI18n() {
	registerMailAutomationCanvasFieldI18nOnce.Do(registerMailAutomationCanvasFieldI18nSeed)
}

var registerMailAutomationCanvasFieldI18nOnce sync.Once

func registerMailAutomationCanvasFieldI18nSeed() {

	registerSeed(Seed{
		Version:   "621c-mail-automation-canvas-field-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('admin.mail.automation_canvas.field.name', 'zh-CN'), ('admin.mail.automation_canvas.field.name', 'en-US'), " +
			"('admin.mail.automation_canvas.field.name_ph', 'zh-CN'), ('admin.mail.automation_canvas.field.name_ph', 'en-US'), " +
			"('admin.mail.automation_canvas.field.trigger', 'zh-CN'), ('admin.mail.automation_canvas.field.trigger', 'en-US'), " +
			"('admin.mail.automation_canvas.badge.new', 'zh-CN'), ('admin.mail.automation_canvas.badge.new', 'en-US'), " +
			"('admin.mail.automation_canvas.side.save_hint', 'zh-CN'), ('admin.mail.automation_canvas.side.save_hint', 'en-US'))",
		SQL: mustSQL("621c_mail_automation_canvas_field_i18n.sql"),
	})
}
