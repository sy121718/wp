package migrations

import "sync"

func registerBlocksOneTableI18n() {
	registerBlocksOneTableI18nOnce.Do(registerBlocksOneTableI18nSeed)
}

var registerBlocksOneTableI18nOnce sync.Once

func registerBlocksOneTableI18nSeed() {

	registerSeed(Seed{
		Version:   "421-i18n-blocks-one-table",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.blocks.impact.badgeTail','admin.blocks.impact.help','admin.blocks.list_empty')",
		SQL: mustSQL("421_i18n_blocks_one_table.sql"),
	})
}

func init() { registerBlocksOneTableI18n() }
