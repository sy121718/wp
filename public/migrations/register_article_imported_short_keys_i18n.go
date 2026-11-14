package migrations

// 753 把 750a 引入的两条点分长 key 原位改名为短 key，并保护运营自定义译文。
//
// 守门只看「库里是否还有已退役的长 key」：有旧行就必须执行搬迁，
// 不能因为短 key 已齐而跳过 —— 旧库升级时短 key 可能已由别的途径补上，
// 此时跳过会把旧译文和旧 key 永久留在库里。
// 搬迁 SQL 自己处理旧新并存（元数据完全一致才去重，否则整批回滚）。
func init() {
	registerSeed(Seed{
		Version:   "753-article-imported-short-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN (
    'admin.article.ok.imported', 'admin.article.edit.visualOpen'))
THEN 0 ELSE 1 END`,
		SQL: mustSQL("753_article_imported_short_keys_i18n.sql"),
	})
}
