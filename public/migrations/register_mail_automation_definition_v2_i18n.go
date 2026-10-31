package migrations

import "sync"

func registerMailAutomationDefinitionV2I18n() {
	registerMailAutomationDefinitionV2I18nOnce.Do(registerMailAutomationDefinitionV2I18nSeed)
}

var registerMailAutomationDefinitionV2I18nOnce sync.Once

func registerMailAutomationDefinitionV2I18nSeed() {

	registerSeed(Seed{
		Version:   "621a-mail-automation-definition-v2-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('admin.mail.detail.unknownOutlet', 'zh-CN'), ('admin.mail.detail.unknownOutlet', 'en-US'), " +
			"('admin.mail.detail.badCondition', 'zh-CN'), ('admin.mail.detail.badCondition', 'en-US'))",
		SQL: mustSQL("621_mail_automation_definition_v2_i18n.sql"),
	})
}
