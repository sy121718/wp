package migrations

func init() {
	registerSeed(Seed{
		Version:   "750a-article-imported-i18n",
		TableName: "sys_i18n",
		// 守门（命中即跳过）分两种跳过情形：
		//   · 库里还有已退役的长 key —— 必须留给 753 做主搬迁，这里先补默认短 key
		//     会让 753 看到「新 key 已齐」而跳过，旧译文永远搬不过来；
		//   · 本批 4 条短 key 已齐 —— 新装重跑不需要再补。
		// 只有「没有旧 key 且短 key 不齐」才执行补种。
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN (
        'admin.article.ok.imported', 'admin.article.edit.visualOpen'))
    OR (SELECT COUNT(*) FROM sys_i18n
        WHERE (item_key, lang) IN (
            ('artImportOk','zh-CN'), ('artImportOk','en-US'),
            ('artVisualOpen','zh-CN'), ('artVisualOpen','en-US'))) = 4
THEN 1 ELSE 0 END`,
		SQL: mustSQL("750a_article_imported_short_keys_i18n.sql"),
	})
}
