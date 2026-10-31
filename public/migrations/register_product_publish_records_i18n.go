package migrations

import "sync"

func init() { registerProductPublishRecordsI18n() }

var productPublishRecordsI18nOnce sync.Once

func registerProductPublishRecordsI18n() {
	productPublishRecordsI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "661-product-publish-records-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 28 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang = 'en-US' AND item_key IN (" +
				"'admin.products.publish.title', 'admin.products.publish.lead', 'admin.products.publish.noHistory', 'admin.products.publish.staleHint'" +
				"'admin.products.publish.programLead', 'admin.products.publish.programLink', 'admin.products.publish.programTail', 'admin.products.publish.notWired'" +
				"'admin.products.publish.notWiredDesc', 'admin.products.publish.totalNote', 'admin.products.publish.ph.keyword', 'admin.products.publish.col.product'" +
				"'admin.products.publish.col.site', 'admin.products.publish.col.online', 'admin.products.publish.col.lang', 'admin.products.publish.col.publishedAt'" +
				"'admin.products.publish.col.artifact', 'admin.products.publish.status.all', 'admin.products.publish.status.active', 'admin.products.publish.status.draft'" +
				"'admin.products.publish.status.none', 'admin.products.publish.status.stale', 'admin.products.publish.pathUnknown', 'admin.products.publish.langNotPublished'" +
				"'admin.products.publish.noInstanceHint', 'admin.products.publish.republish', 'admin.products.publish.emptyHeading', 'admin.products.publish.emptyDesc')",
			SQL: mustSQL("661_product_publish_records_i18n.sql"),
		})
	})
}
