package migrations

import "sync"

// register_page_langs_republish_i18n.go — 语言面板「重新发布」的词条（494，V4）。
//
// 注册方式：由 register.go 的 init() 显式调用（与 492 / 493 同形）。
func registerPageLangsRepublishI18n() {
	registerPageLangsRepublishI18nOnce.Do(registerPageLangsRepublishI18nSeed)
}

// registerPageLangsRepublishI18nOnce 让重复调用成为空操作。
var registerPageLangsRepublishI18nOnce sync.Once

// registerPageLangsRepublishI18nSeed 注册 494（真正干活的那一半）。
func registerPageLangsRepublishI18nSeed() {
	// 494：2 个 key × 2 语言。门槛判据按本批自己的 key 逐条枚举（写进 SQL 字面量）。
	registerSeed(Seed{
		Version:   "494-i18n-page-langs-republish",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.page.langs.action.republish','admin.page.langs.republished'" +
			")",
		SQL: mustSQL("494_page_langs_republish_i18n.sql"),
	})
}
