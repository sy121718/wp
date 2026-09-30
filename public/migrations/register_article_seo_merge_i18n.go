package migrations

// register_article_seo_merge_i18n.go — 482：文章 SEO 字段合并的说明文案。
//
// 注册方式：本文件自带 init()（与 481/480/479 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "482-article-seo-merge-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.article.edit.seoMergedHint'",
		SQL: mustSQL("482_article_seo_merge_i18n.sql"),
	})
}
