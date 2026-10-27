package migrations

// register_page_list_lang_filter_i18n.go — 671：页面列表语言维度筛选的词条（批2）。
//
// 门槛逐条枚举本批的 5 个 key（中英共 10 行）：用 LIKE 或全库计数判，存量库随便一批词条
// 都能满足，缺的词永远补不上（AGENTS.md §数据库「seed 门槛必须逐条枚举」，迁移 494 的坑）。
// 两种语言都数：ConditionSQL 返回 >0 即「已完成 → 跳过」，只数一种语言时另一种缺行仍为 1。

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
