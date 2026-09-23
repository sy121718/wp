package migrations

import "sync"

// register_empty_actions_i18n.go — 列表页空态「下一步动作」的新增词条（415）。
//
// 见 415_i18n_empty_actions.sql 的头部：P1-15 剩下的几页空态只有 title（有的还有 desc），
// 缺 .empty-actions（用户看完「这里空的」之后没有任何可点的下一步）。本轮逐页判断
// 「这一页真正的下一步是什么」后补齐 —— 判断结论与每页的理由写在 SQL 的 remark 列里，
// 给不出有意义动作的档位（analytics 的「还没有工程」档、seo 的体检占位档）刻意**不给**按钮，
// 理由写在模板注释里。
//
// 本批新增 10 个 key × 2 语言 = 20 行：
//
//	admin.roles.perm.empty.action
//	admin.seo.paths.empty.action
//	admin.analytics.no_project.refresh / admin.analytics.empty.action
//	admin.mail.automation_run.timeline.empty.action
//	admin.mail.campaign.empty.action
//	admin.redirect.empty.action / admin.redirect.pick_project.title / admin.redirect.pick_project.action
//	admin.mail.marketing.contacts.empty.clear
//
// departments / menus 的空态动作复用既有 key（admin.depts.action.create / admin.menus.create），
// 不重复登记新词条。
//
// 注册方式：本文件自带 init()（与 register_content_list_filter_i18n.go 同形）。
func registerEmptyActionsI18n() {
	registerEmptyActionsI18nOnce.Do(registerEmptyActionsI18nSeed)
}

// registerEmptyActionsI18nOnce 让重复调用成为空操作。
var registerEmptyActionsI18nOnce sync.Once

// registerEmptyActionsI18nSeed 注册 415（真正干活的那一半）。
func registerEmptyActionsI18nSeed() {
	// 门槛 = 本批 10 个 key 的 **en-US** 行都在（= 10 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过（413 的注释里记过这个坑）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "415-i18n-empty-actions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.roles.perm.empty.action'," +
			"'admin.seo.paths.empty.action'," +
			"'admin.analytics.no_project.refresh','admin.analytics.empty.action'," +
			"'admin.mail.automation_run.timeline.empty.action'," +
			"'admin.mail.campaign.empty.action'," +
			"'admin.redirect.empty.action','admin.redirect.pick_project.title','admin.redirect.pick_project.action'," +
			"'admin.mail.marketing.contacts.empty.clear')",
		SQL: mustSQL("415_i18n_empty_actions.sql"),
	})
}

func init() { registerEmptyActionsI18n() }
