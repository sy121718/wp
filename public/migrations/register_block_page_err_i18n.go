package migrations

import "sync"

func registerBlockPageErrI18n() {
	registerBlockPageErrI18nOnce.Do(registerBlockPageErrI18nSeed)
}

var registerBlockPageErrI18nOnce sync.Once

func registerBlockPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "405-i18n-block-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN ('ErrBlockNotFound','uiInternalErr')",
		SQL: mustSQL("405_i18n_block_page_err.sql"),
	})
}

func init() { registerBlockPageErrI18n() }
