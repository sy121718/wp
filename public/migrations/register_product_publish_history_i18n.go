package migrations

import "sync"

func init() { registerProductPublishHistoryI18n() }

var productPublishHistoryI18nOnce sync.Once

func registerProductPublishHistoryI18n() {
	productPublishHistoryI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "673b-product-publish-history-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 15 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang = 'en-US' AND item_key IN (" +
				"'admin.products.publish.history.title', 'admin.products.publish.history.lead', 'admin.products.publish.history.hint', 'admin.products.publish.history.ok'" +
				"'admin.products.publish.history.failed', 'admin.products.publish.history.trigger', 'admin.products.publish.history.t.create', 'admin.products.publish.history.t.manual'" +
				"'admin.products.publish.history.t.switch', 'admin.products.publish.history.t.url', 'admin.products.publish.history.t.rollback', 'admin.products.publish.history.t.override'" +
				"'admin.products.publish.history.t.auto', 'admin.products.publish.history.more', 'admin.products.publish.history.empty'" +
				")",
			SQL: mustSQL("673b_product_publish_history_i18n.sql"),
		})
	})
}
