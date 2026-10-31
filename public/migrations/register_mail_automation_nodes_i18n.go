package migrations

import (
	"strings"
	"sync"
)

func registerMailAutomationNodesI18n() {
	registerMailAutomationNodesI18nOnce.Do(registerMailAutomationNodesI18nSeed)
}

var registerMailAutomationNodesI18nOnce sync.Once

var mailAutomationNodesI18nKeys = []string{
	"admin.mail.detail.outletMissing",
	"admin.mail.detail.needVariants",
	"admin.mail.detail.variantDuplicate",
	"admin.mail.detail.variantWeightBad",
	"admin.mail.detail.needWebhookURL",
	"admin.mail.detail.webhookDenied",
	"mail.run.branchOut",
	"mail.run.abVariant",
	"mail.run.outletMissing",
	"mail.run.webhookOk",
	"mail.run.webhookFailed",
	"mail.run.webhookNoUrl",
	"mail.run.webhookDenied",
}

func registerMailAutomationNodesI18nSeed() {

	pairs := make([]string, 0, len(mailAutomationNodesI18nKeys)*2)
	for _, key := range mailAutomationNodesI18nKeys {
		pairs = append(pairs, "('"+key+"', 'zh-CN')", "('"+key+"', 'en-US')")
	}
	registerSeed(Seed{
		Version:   "621e-mail-automation-nodes-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 26 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			strings.Join(pairs, ", ") + ")",
		SQL: mustSQL("621e_mail_automation_nodes_i18n.sql"),
	})
}
