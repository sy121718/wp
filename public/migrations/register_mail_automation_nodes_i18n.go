package migrations

import (
	"strings"
	"sync"
)

// register_mail_automation_nodes_i18n.go — 621e：T4 四个新节点的校验明细与运行文案。
//
// 两组 key 同批（因为它们的生产者在同一次提交里落地）：admin.mail.detail.* 是画布保存
// 时回显的参数校验明细，mail.run.* 是执行期写进节点日志的运行文案。
func registerMailAutomationNodesI18n() {
	registerMailAutomationNodesI18nOnce.Do(registerMailAutomationNodesI18nSeed)
}

var registerMailAutomationNodesI18nOnce sync.Once

// mailAutomationNodesI18nKeys 本批的 13 个 key（判据与 SQL 必须枚举同一批）。
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
	// 判据 = 13 个 key × 中英 = 26 行，逐条枚举（上界封闭）。
	//
	// 不用 LIKE 'admin.mail.detail.%' / 'mail.run.%'：两个前缀下已有几十条历史词条，
	// LIKE 计数会被它们喂饱 → 判据恒真 → 本批漏词条也照样绿（迁移 178 踩过同样的坑）。
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
