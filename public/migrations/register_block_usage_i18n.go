package migrations

import "sync"

func registerBlockUsageI18n() {
	registerBlockUsageI18nOnce.Do(registerBlockUsageI18nSeed)
}

var registerBlockUsageI18nOnce sync.Once

func registerBlockUsageI18nSeed() {

	registerSeed(Seed{
		Version:   "297-block-usage-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'MsgBlockUsagePageDocument', 'MsgBlockUsagePageStructure', 'MsgBlockUsagePageRevision', " +
			"'MsgBlockUsageThemeSlot', 'MsgBlockUsageBlockDocument', 'MsgBlockUsageContentTemplate', " +
			"'MsgBlockUsagePresentationInstance')",
		SQL: mustSQL("297_i18n_seed_block_usage_i18n.sql"),
	})
}
