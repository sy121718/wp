package migrations

import "sync"

// register_mail_automation_canvas_field_i18n.go — 621c：画布顶栏名称 / 触发方式控件词条。
//
// 与 621b（措辞修正）同一批：画布成为唯一编辑器之后，新建流程要能在画布上填完名称与
// 触发方式并保存 —— 这四条（+ 一条侧栏提示）是那组控件的文案。
func registerMailAutomationCanvasFieldI18n() {
	registerMailAutomationCanvasFieldI18nOnce.Do(registerMailAutomationCanvasFieldI18nSeed)
}

var registerMailAutomationCanvasFieldI18nOnce sync.Once

func registerMailAutomationCanvasFieldI18nSeed() {
	// 逐条枚举本批 5 个 item_key × 中英 = 10 行（上界封闭）。
	registerSeed(Seed{
		Version:   "621c-mail-automation-canvas-field-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (" +
			"('admin.mail.automation_canvas.field.name', 'zh-CN'), ('admin.mail.automation_canvas.field.name', 'en-US'), " +
			"('admin.mail.automation_canvas.field.name_ph', 'zh-CN'), ('admin.mail.automation_canvas.field.name_ph', 'en-US'), " +
			"('admin.mail.automation_canvas.field.trigger', 'zh-CN'), ('admin.mail.automation_canvas.field.trigger', 'en-US'), " +
			"('admin.mail.automation_canvas.badge.new', 'zh-CN'), ('admin.mail.automation_canvas.badge.new', 'en-US'), " +
			"('admin.mail.automation_canvas.side.save_hint', 'zh-CN'), ('admin.mail.automation_canvas.side.save_hint', 'en-US'))",
		SQL: mustSQL("621c_mail_automation_canvas_field_i18n.sql"),
	})
}
