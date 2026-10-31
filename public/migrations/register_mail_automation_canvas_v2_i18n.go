package migrations

import (
	"strings"
	"sync"
)

func registerMailAutomationCanvasV2I18n() {
	registerMailAutomationCanvasV2I18nOnce.Do(registerMailAutomationCanvasV2I18nSeed)
}

var registerMailAutomationCanvasV2I18nOnce sync.Once

var mailCanvasV2I18nKeys = []string{
	"admin.mail.automation_node.trigger.label",
	"admin.mail.automation_node.trigger.hint",
	"admin.mail.automation_node.delay.label",
	"admin.mail.automation_node.delay.hint",
	"admin.mail.automation_node.email.label",
	"admin.mail.automation_node.email.hint",
	"admin.mail.automation_node.filter.label",
	"admin.mail.automation_node.filter.hint",
	"admin.mail.automation_node.list_status_branch.label",
	"admin.mail.automation_node.list_status_branch.hint",
	"admin.mail.automation_node.ab_test.label",
	"admin.mail.automation_node.ab_test.hint",
	"admin.mail.automation_node.webhook.label",
	"admin.mail.automation_node.webhook.hint",
	"admin.mail.automation_node.tag.label",
	"admin.mail.automation_node.tag.hint",
	"admin.mail.automation_node.end.label",
	"admin.mail.automation_node.end.hint",
	"admin.mail.automation_canvas.palette.head",
	"admin.mail.automation_canvas.palette.aria",
	"admin.mail.automation_canvas.undo",
	"admin.mail.automation_canvas.redo",
	"admin.mail.automation_canvas.fit",
	"admin.mail.automation_canvas.fit_label",
	"admin.mail.automation_canvas.mini.title",
	"admin.mail.automation_canvas.delete_node",
	"admin.mail.automation_canvas.kbd.undo",
	"admin.mail.automation_canvas.kbd.del",
	"admin.mail.automation_canvas.kbd.save",
	"admin.mail.automation_canvas.email_disabled",
}

func registerMailAutomationCanvasV2I18nSeed() {

	registerSeed(Seed{
		Version:      "621d-mail-automation-canvas-v2-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: mailCanvasV2I18nConditionSQL(),
		SQL:          mustSQL("621d_mail_automation_canvas_v2_i18n.sql"),
	})
}

func mailCanvasV2I18nConditionSQL() string {
	pairs := make([]string, 0, len(mailCanvasV2I18nKeys)*2)
	for _, key := range mailCanvasV2I18nKeys {
		pairs = append(pairs, "('"+key+"', 'zh-CN')", "('"+key+"', 'en-US')")
	}
	return "SELECT CASE WHEN COUNT(*) >= 60 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
		strings.Join(pairs, ", ") + ")"
}
