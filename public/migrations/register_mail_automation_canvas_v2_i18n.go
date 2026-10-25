package migrations

import (
	"strings"
	"sync"
)

// register_mail_automation_canvas_v2_i18n.go — 621d：画布重写（T3）的界面词条。
//
// 两组 key：automation_node.*（十个节点类型的名字与提示）与 automation_canvas.*
// （画布新控件）。旧命名空间 automation_edit.* 是表单页的，那个页面已删。
func registerMailAutomationCanvasV2I18n() {
	registerMailAutomationCanvasV2I18nOnce.Do(registerMailAutomationCanvasV2I18nSeed)
}

var registerMailAutomationCanvasV2I18nOnce sync.Once

// mailCanvasV2I18nKeys 本批的 key 清单（判据与 SQL 必须枚举同一批）。
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
	// 判据 = 本批 30 个 key × 中英 = 60 行**逐条枚举**（上界封闭）。
	//
	// 不用 LIKE 'admin.mail.automation_node.%'：automation_canvas.* 前缀下已有 190/232/450
	// 种下的二十多条历史词条，LIKE 计数会被它们喂饱 → 判据恒真 → 本批漏词条也照样绿。
	registerSeed(Seed{
		Version:      "621d-mail-automation-canvas-v2-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: mailCanvasV2I18nConditionSQL(),
		SQL:          mustSQL("621d_mail_automation_canvas_v2_i18n.sql"),
	})
}

// mailCanvasV2I18nConditionSQL 拼判据（(item_key, lang) IN (...)，期望 60 行）。
func mailCanvasV2I18nConditionSQL() string {
	pairs := make([]string, 0, len(mailCanvasV2I18nKeys)*2)
	for _, key := range mailCanvasV2I18nKeys {
		pairs = append(pairs, "('"+key+"', 'zh-CN')", "('"+key+"', 'en-US')")
	}
	return "SELECT CASE WHEN COUNT(*) >= 60 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
		strings.Join(pairs, ", ") + ")"
}
