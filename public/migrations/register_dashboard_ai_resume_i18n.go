package migrations

import "sync"

// register_dashboard_ai_resume_i18n.go — 概览页「继续上次对话」一词条（576）。
//
// 它是「概览页默认收起回答」这条设计的配套入口：历史仍在加载，但要用户点一下
// 才铺开（见 ai-fab.js 的 loadHistory 与 [data-ai-fab-resume] 绑定）。
func registerDashboardAIResumeI18n() {
	registerDashboardAIResumeI18nOnce.Do(registerDashboardAIResumeI18nSeed)
}

var registerDashboardAIResumeI18nOnce sync.Once

func registerDashboardAIResumeI18nSeed() {
	registerSeed(Seed{
		Version:   "576-dashboard-ai-resume-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("576_dashboard_ai_resume_i18n.sql"),
		// 判据枚举本批自己的一条 key × 两种语言 = 2 行（上界封闭，不用 LIKE 前缀）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.resume') AND lang IN ('zh-CN', 'en-US')",
	})
}
