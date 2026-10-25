package migrations

import "sync"

// register_mail_automation_frequency_i18n.go — 623a：回信退出的运行文案。
//
// 与 623（两列 + 索引）是同一批的两半：623 给**行为**，623a 给**说法**。
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
