package migrations

import "sync"

func init() { registerProductPublishHistoryI18n() }

var productPublishHistoryI18nOnce sync.Once

func registerProductPublishHistoryI18n() {
	productPublishHistoryI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "673b-product-publish-history-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('pPubHistEmpty'),
    ('pPubHistFail'),
    ('pPubHistHint'),
    ('pPubHistLead'),
    ('pPubHistMore'),
    ('pPubHistOk'),
    ('pPubHistTAuto'),
    ('pPubHistTManual'),
    ('pPubHistTNew'),
    ('pPubHistTSwitch'),
    ('pPubHistTUndo'),
    ('pPubHistTUrl'),
    ('pPubHistTitle'),
    ('pPubHistTrigger'),
    ('pPubOverride')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
			SQL: mustSQL("723z_current_673b_product_publish_history_i18n.sql"),
		})
	})
}
