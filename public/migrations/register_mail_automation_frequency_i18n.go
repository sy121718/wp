package migrations

import "sync"

func registerMailAutomationFrequencyI18n() {
	registerMailAutomationFrequencyI18nOnce.Do(registerMailAutomationFrequencyI18nSeed)
}

var registerMailAutomationFrequencyI18nOnce sync.Once

func registerMailAutomationFrequencyI18nSeed() {
	registerSeed(Seed{
		Version:   "623a-mail-automation-frequency-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('mail.run.stoppedByReply', 'zh-CN'), ('mail.run.stoppedByReply', 'en-US'))",
		SQL: mustSQL("623a_mail_automation_frequency_i18n.sql"),
	})
}
