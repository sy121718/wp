package migrations

import "sync"

func registerMailPageNoticeI18n() {
	registerMailPageNoticeI18nOnce.Do(registerMailPageNoticeI18nSeed)
}

var registerMailPageNoticeI18nOnce sync.Once

func registerMailPageNoticeI18nSeed() {

	registerSeed(Seed{
		Version:   "614-mail-page-notice-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('mail.bulk.allDone', 'zh-CN'), ('mail.bulk.allDone', 'en-US'), " +
			"('mail.bulk.allSkipped', 'zh-CN'), ('mail.bulk.allSkipped', 'en-US'), " +
			"('mail.bulk.partial', 'zh-CN'), ('mail.bulk.partial', 'en-US'), " +
			"('mail.msg.automationTick', 'zh-CN'), ('mail.msg.automationTick', 'en-US'), " +
			"('mail.page.templateListFailed', 'zh-CN'), ('mail.page.templateListFailed', 'en-US'), " +
			"('mail.page.contactStatusBad', 'zh-CN'), ('mail.page.contactStatusBad', 'en-US'), " +
			"('mail.page.campaignIDRequired', 'zh-CN'), ('mail.page.campaignIDRequired', 'en-US'), " +
			"('mail.page.runIDRequired', 'zh-CN'), ('mail.page.runIDRequired', 'en-US'), " +
			"('mail.page.automationIDRequired', 'zh-CN'), ('mail.page.automationIDRequired', 'en-US'))",
		SQL: mustSQL("614_mail_page_notice_i18n.sql"),
	})
}
