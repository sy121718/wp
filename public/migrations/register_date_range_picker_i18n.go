package migrations

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
