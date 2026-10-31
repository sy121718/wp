package migrations

import "sync"

func init() { registerPageBulkNoticeI18n() }

var registerPageBulkNoticeI18nOnce sync.Once

func registerPageBulkNoticeI18n() {
	registerPageBulkNoticeI18nOnce.Do(registerPageBulkNoticeI18nSeed)
}

func registerPageBulkNoticeI18nSeed() {

	registerSeed(Seed{
		Version:   "510-i18n-page-bulk-notice",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.pages.bulk.nothing','admin.pages.bulk.deleted'," +
			"'admin.pages.bulk.skipped','admin.pages.bulk.partial'" +
			")",
		SQL: mustSQL("510_page_bulk_notice_i18n.sql"),
	})
}
