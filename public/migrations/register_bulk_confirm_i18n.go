package migrations

import "sync"

func registerBulkConfirmI18n() {
	registerBulkConfirmI18nOnce.Do(registerBulkConfirmI18nSeed)
}

var registerBulkConfirmI18nOnce sync.Once

func registerBulkConfirmI18nSeed() {

	registerSeed(Seed{
		Version:   "434-i18n-bulk-confirm",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 36 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.article.list.bulkDeleteConfirm','admin.blocks.bulk_delete_confirm'," +
			"'admin.common.bulk.column','admin.content.templates.bulkDeleteConfirm'," +
			"'admin.coupons.bulk.delete','admin.coupons.bulkDeleteConfirm'," +
			"'admin.coupons.bulk.target','admin.coupons.bulk.toggle'," +
			"'admin.customers.bulk.disable','admin.customers.bulk.disableConfirm'," +
			"'admin.customers.bulk.enable','admin.customers.bulk.enableConfirm'," +
			"'admin.customers.bulk.unlock','admin.customers.bulk.unlockConfirm'," +
			"'admin.inventory_sources.bulkDeleteConfirm','admin.inventory.warehouses.bulkDeleteConfirm'," +
			"'admin.mail.accounts.bulk_delete_confirm','admin.mail.marketing.bulk_status_confirm'," +
			"'admin.mail.marketing.bulk_status_submit','admin.mail.marketing.campaigns.bulk_delete_confirm'," +
			"'admin.mail.templates.bulk_delete_confirm','admin.orders.bulk.cancel'," +
			"'admin.orders.bulkCancelConfirm','admin.orders.bulk.status'," +
			"'admin.orders.bulk.target','admin.orders.ph.bulk_remark'," +
			"'admin.pages.bulk_delete_confirm','admin.product_attributes.bulkDeleteConfirm'," +
			"'admin.product_brands.bulkDeleteConfirm','admin.products.bulkDeleteConfirm'," +
			"'admin.product_tags.bulkDeleteConfirm','admin.redirect.bulk_delete_confirm'," +
			"'admin.returns.bulk.approve','admin.returns.bulk.reject'," +
			"'admin.returns.bulkRejectConfirm','admin.returns.ph.bulk_remark')",
		SQL: mustSQL("434_i18n_bulk_confirm.sql"),
	})
}

func init() { registerBulkConfirmI18n() }
