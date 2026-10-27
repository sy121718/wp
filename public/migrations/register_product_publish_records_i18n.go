package migrations

// register_product_publish_records_i18n.go — 661 的注册项（B9 发布记录页）。
//
// 判据逐条枚举本批 28 个 key 的 en-US 行（门槛 COUNT >= 28）。挑 en-US 而不是 zh-CN：
// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在「本批还没跑」
// 时就成立、整批词条被静默跳过（432 / 643 / 660 的同一理由）。
//
// 不用前缀 LIKE：将来任何新增的 admin.products.publish.* 都会把这条门槛提前满足 ——
// 而这条 seed 的职责恰好是「这一屏现在要用的 28 个 key 一个都不少」。

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
