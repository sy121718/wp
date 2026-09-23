package migrations

import "sync"

// register_analytics_view_tabs_i18n.go — 访问统计页维度页签的词条（427）。
//
// 见 427_analytics_view_tabs_i18n.sql 的头部：本批只新增 1 个 key
// （admin.analytics.view.label，即 tablist 的可访问名）。5 个标签的文字与三个维度的口径说明
// 全部复用既有词条，不重复登记。
//
// 注册方式：本文件自带 init()（与 register_empty_actions_i18n.go 同形）。
func registerAnalyticsViewTabsI18n() {
	registerAnalyticsViewTabsI18nOnce.Do(registerAnalyticsViewTabsI18nSeed)
}

// registerAnalyticsViewTabsI18nOnce 让重复调用成为空操作。
var registerAnalyticsViewTabsI18nOnce sync.Once

// registerAnalyticsViewTabsI18nSeed 注册 427（真正干活的那一半）。
func registerAnalyticsViewTabsI18nSeed() {
	// 门槛 = 本批唯一 key 的 **en-US** 行在（= 1 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与模板里的 t() 兜底同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（413 / 415 的注释里各记过一次这个坑）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条 seed 每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "427-analytics-view-tabs-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN ('admin.analytics.view.label')",
		SQL: mustSQL("427_analytics_view_tabs_i18n.sql"),
	})
}

func init() { registerAnalyticsViewTabsI18n() }
