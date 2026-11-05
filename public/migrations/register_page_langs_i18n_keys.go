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
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.page.langs.action.exclude'),
    ('admin.page.langs.action.keep'),
    ('admin.page.langs.action.restore'),
    ('admin.page.langs.col.actions'),
    ('admin.page.langs.col.lang'),
    ('admin.page.langs.col.note'),
    ('admin.page.langs.col.status'),
    ('admin.page.langs.empty'),
    ('admin.page.langs.excluded'),
    ('admin.page.langs.note'),
    ('admin.page.langs.note.default'),
    ('admin.page.langs.restored'),
    ('admin.page.langs.status.draft'),
    ('admin.page.langs.status.excluded'),
    ('admin.page.langs.status.published'),
    ('admin.page.langs.title'),
    ('admin.page.translation_misses.filter_project'),
    ('admin.page.translation_misses.filter_submit'),
    ('admin.pages.action.langs'),
    ('pSeoDrawerHint')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723p_current_505_page_langs_i18n_keys.sql"),
	})
}
