package migrations

import "sync"

// register_page_langs_i18n_keys.go — 语言产出面板与另 4 处「key 被用但没 seed」的词条（505，批次 2 FIX-11 / FIX-12）。
//
// 为什么单独一条迁移：这 20 个 key 在模板 / handler 里用了很久，但**没有任何 seed**，
// 英文界面命中失败后回落源码中文兜底 —— 不报错、不 500、日志干净，默认语言下完全看不出来。
// 门禁 scripts/check-i18n-keys-seeded.sh（同批接入 CI）扫的就是这个方向。
//
// 注册方式：本文件**自带 func init() 自注册**，不在 register.go 的 init() 里再挂一处 ——
// register.go 是所有人加迁移都会碰的热点文件，并行批次下极易撞车；而「谁负责注册」仍只有一个真源
// （只有这里这一处调用，sync.Once 兜住重复）。399 的 register_empty_actions_i18n.go 是同一种写法。
func init() { registerPageLangsI18nKeys() }

// registerPageLangsI18nKeys 注册 505。
func registerPageLangsI18nKeys() {
	registerPageLangsI18nKeysOnce.Do(registerPageLangsI18nKeysSeed)
}

// registerPageLangsI18nKeysOnce 让重复调用成为空操作。
var registerPageLangsI18nKeysOnce sync.Once

// registerPageLangsI18nKeysSeed 注册 505（真正干活的那一半）。
func registerPageLangsI18nKeysSeed() {
	// 505：20 个 key × 2 语言 = 40 行。
	// 门槛判据**逐条枚举本批自己的 key**（写进 SQL 字面量，不用 LIKE 前缀、不用全库总量），上界封闭。
	// 判据形态说明：sys_i18n 主键是 (item_key, lang) —— 同一 key 同一语言至多一行，于是
	// 「COUNT(DISTINCT item_key) >= 20 且 COUNT(*) >= 40」严格等价于「20 个 key 的中英两行都齐」
	// （distinct 到 20 而总行数不足 40，必然意味着某条 key 缺一种语言）。
	// 偏差方向刻意选「宁可重跑，不可静默跳过」：少一行（被运维删掉 / 只补了一半）→ 条件不成立 → 下次启动重跑补回。
	//
	// 写法注意：Go 里单引号是 rune 字面量，SQL 的 'x' 必须整段嵌在**双引号**的 Go 字符串里，
	// 续行只在行尾留 " +（不要把逗号写在 + 之前，那会被解析成 struct literal 的另一个元素）。
	registerSeed(Seed{
		Version:   "505-i18n-page-langs-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 20 AND COUNT(*) >= 40 THEN 1 ELSE 0 END " +
			"FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN (" +
			"'admin.page.langs.title','admin.page.langs.col.lang'," +
			"'admin.page.langs.col.status','admin.page.langs.col.note'," +
			"'admin.page.langs.col.actions','admin.page.langs.empty'," +
			"'admin.page.langs.action.exclude','admin.page.langs.action.restore'," +
			"'admin.page.langs.action.keep','admin.page.langs.note'," +
			"'admin.page.langs.excluded','admin.page.langs.restored'," +
			"'admin.page.langs.status.excluded','admin.page.langs.status.published'," +
			"'admin.page.langs.status.draft','admin.page.langs.note.default'," +
			"'admin.page.translation_misses.filter_project','admin.page.translation_misses.filter_submit'," +
			"'admin.pages.action.langs','admin.products.seo.drawerHint')",
		SQL: mustSQL("505_page_langs_i18n_keys.sql"),
	})
}
