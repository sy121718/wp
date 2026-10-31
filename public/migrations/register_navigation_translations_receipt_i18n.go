package migrations

func init() {
	registerSeed(Seed{
		Version:   "702-navigation-translations-receipt-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
			WHERE lang IN ('zh-CN','en-US') AND (
				(item_key = 'admin.navigation_translations.saved'
				 AND item_value IN (
					'已保存 %s 条译文；已标记受影响页面待重建（下次构建生效）。',
					'Saved %s translation(s); affected pages are marked for rebuild (effective on the next build).'))
				OR
				(item_key = 'admin.navigation_translations.savedNone'
				 AND item_value IN (
					'没有需要写入的变化。',
					'There were no changes to write.'))
			)`,
		SQL: mustSQL("702_navigation_translations_receipt_i18n.sql"),
	})
}
