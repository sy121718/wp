package migrations

import "sync"

var registerPageListLangFilterI18nOnce sync.Once

func init() { registerPageListLangFilterI18n() }

func registerPageListLangFilterI18n() {
	registerPageListLangFilterI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "671-page-list-lang-filter-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n
				WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (
					'admin.pages.filter_lang',
					'admin.pages.filter_lang_all',
					'admin.pages.filter_lang_state',
					'admin.pages.empty_lang_title',
					'admin.pages.empty_lang_desc'
				)`,
			SQL: mustSQL("671_page_list_lang_filter_i18n.sql"),
		})
	})
}
