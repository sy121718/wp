package migrations

func init() {
	registerSeed(Seed{
		Version:   "482-article-seo-merge-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.article.edit.seoMergedHint'",
		SQL: mustSQL("482_article_seo_merge_i18n.sql"),
	})
}
