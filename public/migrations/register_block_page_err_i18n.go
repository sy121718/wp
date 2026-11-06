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
		// 只核对本批真正播种的对象：共享归口 key（uiInternalErr）已改由 747/738 负责，
		// 不再算进 405 的守门。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key = 'ErrBlockNotFound'",
		SQL: mustSQL("747c_current_405_i18n_block_page_err.sql"),
	})
}

func init() { registerBlockPageErrI18n() }
