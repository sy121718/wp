package migrations

import "sync"

func init() { registerPageLangsI18nKeys() }

func registerPageLangsI18nKeys() {
	registerPageLangsI18nKeysOnce.Do(registerPageLangsI18nKeysSeed)
}

var registerPageLangsI18nKeysOnce sync.Once

func registerPageLangsI18nKeysSeed() {

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
