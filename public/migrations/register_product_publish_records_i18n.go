package migrations

import "sync"

func init() { registerProductPublishRecordsI18n() }

var productPublishRecordsI18nOnce sync.Once

func registerProductPublishRecordsI18n() {
	productPublishRecordsI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "661-product-publish-records-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.products.publish.noHistory'),
    ('admin.products.publish.ph.keyword'),
    ('admin.products.publish.programLead'),
    ('admin.products.publish.programLink'),
    ('admin.products.publish.programTail'),
    ('pPubActive'),
    ('pPubArtifact'),
    ('pPubEmptyDesc'),
    ('pPubEmptyHead'),
    ('pPubLang'),
    ('pPubLangNoLive'),
    ('pPubLead'),
    ('pPubLiveAt'),
    ('pPubNoInstHint'),
    ('pPubOnline'),
    ('pPubPathUnknown'),
    ('pPubProd'),
    ('pPubRepublish'),
    ('pPubSite'),
    ('pPubStaleHint'),
    ('pPubStatusAll'),
    ('pPubStatusDraft'),
    ('pPubStatusNone'),
    ('pPubStatusStale'),
    ('pPubTitle'),
    ('pPubTotalNote')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
			SQL: mustSQL("723aa_current_661_product_publish_records_i18n.sql"),
		})
	})
}
