package migrations

// register_product_publish_history_i18n.go — 673b 的注册项（B11 发布历史）。
//
// 判据逐条枚举本批 15 个 key 的 en-US 行（门槛 COUNT >= 15）。挑 en-US 而不是 zh-CN：
// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在「本批还没跑」
// 时就成立、整批词条被静默跳过（432 / 643 / 660 / 661 的同一理由）。
//
// 不用前缀 LIKE：将来任何新增的 admin.products.publish.history.* 都会把这条门槛提前满足。

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
