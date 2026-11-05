package migrations

import "sync"

// registerAICreateRefreshI18n 登记 AI 新建供应商成功后的手动刷新入口文案（713）。
func registerAICreateRefreshI18n() {
	registerAICreateRefreshI18nOnce.Do(registerAICreateRefreshI18nSeed)
}

var registerAICreateRefreshI18nOnce sync.Once

func registerAICreateRefreshI18nSeed() {
	registerSeed(Seed{
		Version:   "713-ai-create-refresh-i18n",
		TableName: "sys_i18n",
		// 门槛只枚举本批自己的两个 (item_key, lang) 元组：中英全齐才算完成。
		// 只数 zh-CN 一条时 en-US 缺了门槛照样成立，英文界面就永远缺词。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.ai.create.refresh', 'zh-CN'), " +
			"('admin.ai.create.refresh', 'en-US'))",
		SQL: mustSQL("713_ai_create_refresh_i18n.sql"),
	})
}
