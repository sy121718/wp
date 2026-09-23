package migrations

func init() {
	registerSeed(Seed{
		Version:   "439-i18n-translation-filter",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 30 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.article.translations.filterKeyword','admin.article.translations.filterPlaceholder'," +
			"'admin.article.translations.noMatch.title','admin.article.translations.noMatch.desc'," +
			"'admin.article.translations.noMatch.action','admin.article.translations.empty.action'," +
			"'admin.navigation_translations.filter_keyword','admin.navigation_translations.filter_placeholder'," +
			"'admin.navigation_translations.no_project.title','admin.navigation_translations.no_project.desc'," +
			"'admin.navigation_translations.no_project.action','admin.navigation_translations.no_match.title'," +
			"'admin.navigation_translations.no_match.desc','admin.navigation_translations.no_match.action'," +
			"'admin.navigation_translations.empty.action')",
		SQL: mustSQL("439_i18n_translation_filter.sql"),
	})
}
