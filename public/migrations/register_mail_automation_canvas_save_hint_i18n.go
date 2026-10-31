package migrations

import "sync"

func registerMailAutomationCanvasSaveHintI18n() {
	registerMailAutomationCanvasSaveHintI18nOnce.Do(registerMailAutomationCanvasSaveHintI18nSeed)
}

var registerMailAutomationCanvasSaveHintI18nOnce sync.Once

func registerMailAutomationCanvasSaveHintI18nSeed() {
	registerSeed(Seed{
		Version:   "621b-mail-automation-canvas-save-hint-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key, lang, item_value) IN (" +
			"('admin.mail.automation_canvas.save_hint.lead', 'zh-CN', '改完点下面的「保存结构」——服务端会走'), " +
			"('admin.mail.automation_canvas.save_hint.strong', 'en-US', 'the same graph validation'))",
		SQL: mustSQL("621b_mail_automation_canvas_save_hint_i18n.sql"),
	})
}
