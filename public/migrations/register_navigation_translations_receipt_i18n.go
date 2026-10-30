package migrations

// 702：导航译文保存回执的两条整句词条（见 702_navigation_translations_receipt_i18n.sql）。
//
// 门槛逐条枚举本批的 4 行（2 个 key × 2 语言），不写 LIKE、也不比全库总量：
// 「表里有多少行」与「本批这几条是不是这两句」是两件事，后者才是这条 seed 的语义。
//
// 与 451 的关系：451 已 seed 过这两个 key，但那条 saved 的 zh-CN 原文用的是 %d，
// 而出口（pkg/response.ArgsText）只认 %s 位置参数 —— 形状不兼容，因此本批覆盖。
// ConditionSQL 判的是**目标原文**而不是「行数存在」：行数一直是 4（451 造的），
// 若只判行数，这条 seed 会在任何库上都判「已应用」而永不执行覆盖。
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
