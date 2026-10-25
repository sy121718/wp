package migrations

import "sync"

// register_mail_automation_frequency_field_i18n.go — 623b：画布的频率 / 回信即退出控件词条。
func registerMailAutomationFrequencyFieldI18n() {
	registerMailAutomationFrequencyFieldI18nOnce.Do(registerMailAutomationFrequencyFieldI18nSeed)
}

var registerMailAutomationFrequencyFieldI18nOnce sync.Once

func registerMailAutomationFrequencyFieldI18nSeed() {
	registerSeed(Seed{
		Version:   "623b-mail-automation-frequency-field-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('admin.mail.automation_canvas.field.frequency', 'zh-CN'), ('admin.mail.automation_canvas.field.frequency', 'en-US'), " +
			"('admin.mail.automation_canvas.frequency.every_time', 'zh-CN'), ('admin.mail.automation_canvas.frequency.every_time', 'en-US'), " +
			"('admin.mail.automation_canvas.frequency.once', 'zh-CN'), ('admin.mail.automation_canvas.frequency.once', 'en-US'), " +
			"('admin.mail.automation_canvas.field.exit_on_reply', 'zh-CN'), ('admin.mail.automation_canvas.field.exit_on_reply', 'en-US'))",
		SQL: mustSQL("623b_mail_automation_frequency_field_i18n.sql"),
	})
}
