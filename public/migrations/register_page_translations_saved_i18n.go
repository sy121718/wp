package migrations

import "sync"

// register_page_translations_saved_i18n.go — 翻译工作台保存回执的整句化（迁移 613）。
//
// 单独成一个主题文件：本批不只是「补两条词条」，还**删掉** 192 号迁移落的三段式词条 ——
// 加与删要落在同一个条件里，否则会出现「新句已入库、旧三段还在」的中间态，
// 而那个中间态正是两份真源并存的窗口（见 SQL 文件头）。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerPageTranslationsSavedI18n() {
	registerPageTranslationsSavedI18nOnce.Do(registerPageTranslationsSavedI18nSeed)
}

var registerPageTranslationsSavedI18nOnce sync.Once

func registerPageTranslationsSavedI18nSeed() {
	// 613：2 个 key × 中英 = 4 行的**新增**部分，判据按这 4 个 (item_key, lang) 组合枚举。
	//
	// 为什么判据只数新增的 4 行、不把 DELETE 的旧三条也纳进来：
	// 旧行在条件成立时已被删掉，把它们写进判据会让条件**永远不成立**（计数少 6），
	// 每次启动都重跑一遍 DELETE —— 幂等是幂等，但「每次启动都改表」本身是噪音，
	// 而 AGENTS.md 要的是「宁可重跑，不可静默跳过」落在**本批新增的对象**上。
	//
	// 4 项齐全时判据为 1（跳过整段 SQL，含 DELETE）；缺任意一 (key, lang) 即为 0（重跑）——
	// 重跑时 DELETE 会对已不存在的行为 0 行、INSERT 走 ON CONFLICT DO NOTHING 只补缺行。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言
	// （用例 register_page_translations_saved_i18n_test.go 会核对判据与 SQL 的组合集合一致）。
	registerSeed(Seed{
		Version:   "613-page-translations-saved-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.page_translations.saved', 'zh-CN'), ('admin.page_translations.saved', 'en-US'), " +
			"('admin.page_translations.saved_rebuild', 'zh-CN'), ('admin.page_translations.saved_rebuild', 'en-US'))",
		SQL: mustSQL("613_page_translations_saved_i18n.sql"),
	})
}
