package migrations

import "sync"

func registerBulkNoticeI18n() {
	registerBulkNoticeI18nOnce.Do(registerBulkNoticeI18nSeed)
}

var registerBulkNoticeI18nOnce sync.Once

func registerBulkNoticeI18nSeed() {

	registerSeed(Seed{
		Version:   "283-bulk-notice-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 60 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.bulk.done', 'admin.bulk.noun.admin', 'admin.bulk.noun.datarule', " +
			"'admin.bulk.noun.dept', 'admin.bulk.noun.menu', 'admin.bulk.noun.permission', " +
			"'admin.bulk.noun.role', 'admin.bulk.partial', 'admin.i18nBulk.allDeleted', " +
			"'admin.i18nBulk.allSkipped', 'admin.i18nBulk.noneSelected', 'admin.i18nBulk.partial', " +
			"'admin.inventory.bulk.sourceDone', 'admin.inventory.bulk.sourcePartial', 'content.bulk.allDeleted', " +
			"'content.bulk.allSkipped', 'content.bulk.noneSelected', 'content.bulk.partial', " +
			"'order.bulk.allDone', 'order.bulk.allSkipped', 'order.bulk.couponTargetInvalid', " +
			"'order.bulk.noneSelected', 'order.bulk.noun.coupon', 'order.bulk.noun.order', " +
			"'order.bulk.noun.return', 'order.bulk.partial', 'order.bulk.verb.approved', " +
			"'order.bulk.verb.cancelled', 'order.bulk.verb.deleted', 'order.bulk.verb.disabled', " +
			"'order.bulk.verb.enabled', 'order.bulk.verb.flowed', 'order.bulk.verb.rejected', " +
			"'page.bulk.pageAllDeleted', 'page.bulk.pageAllSkipped', 'page.bulk.pageMissingID', " +
			"'page.bulk.pageNoneSelected', 'page.bulk.pagePartial', 'page.bulk.redirectAllDeleted', " +
			"'page.bulk.redirectAllSkipped', 'page.bulk.redirectNoneSelected', 'page.bulk.redirectPartial', " +
			"'pBulkAttrDone', 'pAttrPartial', 'pBulkBrandDone', " +
			"'pBrandPartial', 'pBulkCatDone', 'pBulkCatPartial', " +
			"'product.bulk.pricingAllSkip', 'product.bulk.pricingApplied', 'product.bulk.pricingNoChange', " +
			"'pPriceNoneSel', 'product.bulk.pricingPartial', 'pBulkProdDone', " +
			"'pProductPartial', 'pBulkTagDone', 'pBulkTagPartial', " +
			"'pVarNoChange', 'pVarSaved', 'pVarSkipped')",
		SQL: mustSQL("718e_current_283_bulk_notice_i18n.sql"),
	})
}
