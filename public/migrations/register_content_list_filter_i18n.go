package migrations

import "sync"

func registerContentListFilterI18n() {
	registerContentListFilterI18nOnce.Do(registerContentListFilterI18nSeed)
}

var registerContentListFilterI18nOnce sync.Once

func registerContentListFilterI18nSeed() {

	registerSeed(Seed{
		Version:   "413-i18n-content-list-filter",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 14 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.article.list.filterLabel','admin.article.list.filterPlaceholder','admin.article.list.filterEmpty'," +
			"'admin.pages.filter_project','admin.pages.filter_submit','admin.pages.filter_reset'," +
			"'admin.pages.empty_project_title','admin.pages.empty_project_desc'," +
			"'admin.blocks.filter_keyword','admin.blocks.filter_placeholder','admin.blocks.filter_empty'," +
			"'admin.navigations.filter_keyword','admin.navigations.filter_placeholder','admin.navigations.filter_empty')",
		SQL: mustSQL("413_i18n_content_list_filter.sql"),
	})
}

func init() { registerContentListFilterI18n() }
