package migrations

// 635：时间筛选条改成区间控件后的词条（见 635_date_range_picker_i18n.sql）。
//
// 门槛逐条枚举本批的 8 行（4 个 key × 2 语言），不写 LIKE、也不比全库总量：
// 「表里有多少行」与「本批这几条在不在」是两件事，后者才是这条 seed 的语义。
func registerDateRangePickerI18n() {
	registerSeed(Seed{
		Version:   "635-date-range-picker-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.common.filter.placeholder','admin.common.filter.sep'," +
			"'admin.common.filter.clear','admin.common.filter.preset.halfYear')",
		SQL: mustSQL("635_date_range_picker_i18n.sql"),
	})
}
