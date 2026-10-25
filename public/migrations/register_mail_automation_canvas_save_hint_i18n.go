package migrations

import "sync"

// register_mail_automation_canvas_save_hint_i18n.go — 621b：画布保存提示的措辞修正。
//
// 190 seed 的两条词条说「和表单页同一套图校验」，而表单编辑页已删除（画布是唯一编辑器）。
// 历史迁移不改（public/CLAUDE.md），所以用一条 UPDATE 覆盖已安装库的旧值。
//
// 幂等：判据 = 两条词条是否**已经是新值**；SQL 自身的 WHERE 里还带 `item_value = 旧值`，
// 所以运营手改过的行不会被覆盖，而判据依旧成立（不会每次启动重跑）。
func registerMailAutomationCanvasSaveHintI18n() {
	registerMailAutomationCanvasSaveHintI18nOnce.Do(registerMailAutomationCanvasSaveHintI18nSeed)
}

var registerMailAutomationCanvasSaveHintI18nOnce sync.Once

func registerMailAutomationCanvasSaveHintI18nSeed() {
	registerSeed(Seed{
		Version:   "621b-mail-automation-canvas-save-hint-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key, lang, item_value) IN (" +
			"('admin.mail.automation_canvas.save_hint.lead', 'zh-CN', '改完点下面的「保存结构」——服务端会走'), " +
			"('admin.mail.automation_canvas.save_hint.strong', 'en-US', 'the same graph validation'))",
		SQL: mustSQL("621b_mail_automation_canvas_save_hint_i18n.sql"),
	})
}
