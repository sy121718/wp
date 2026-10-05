package migrations

import "sync"

// register_ai_fab_dismiss_i18n.go — 「收起回答」与「回概览继续」两条词条（573）。
//
// 它们是同一个入口的两个方向：概览页的提问框与全局悬浮球是同一段会话，
// 前一条把回答收起来交还卡片，后一条从别的页面回到概览接着问。
func registerAIFabDismissI18n() {
	registerAIFabDismissI18nOnce.Do(registerAIFabDismissI18nSeed)
}

var registerAIFabDismissI18nOnce sync.Once

func registerAIFabDismissI18nSeed() {
	registerSeed(Seed{
		Version:   "573-ai-fab-dismiss-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("573_ai_fab_dismiss_i18n.sql"),
		// 判据取本批自己的两条 key × 两种语言 = 4 行。用 >= 而不是 = ：
		// 将来若有人往同一条迁移里补词条，等号会让判据从「已执行」变成
		// 「未执行」而整条重跑（虽然 ON CONFLICT 兜住了，但那等于每次启动都白跑一遍）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.dismiss', 'admin.ai.fab.backToDashboard') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
