package migrations

import "sync"

func registerAdminCouponsWriteUII18n() {
	registerAdminCouponsWriteUII18nOnce.Do(registerAdminCouponsWriteUII18nSeed)
}

var registerAdminCouponsWriteUII18nOnce sync.Once

func registerAdminCouponsWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "603-admin-coupons-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.coupons.bulk.noneSelected', 'admin.coupons.bulk.refresh', " +
			"'admin.coupons.bulkDelete.allDeleted', 'admin.coupons.bulkDelete.allSkipped', " +
			"'admin.coupons.bulkDelete.partial', 'admin.coupons.bulkToggle.allSkipped', " +
			"'admin.coupons.bulkToggle.allUpdated', 'admin.coupons.bulkToggle.partial', " +
			"'admin.coupons.bulkToggleConfirm', 'admin.coupons.write.noJs')",
		SQL: mustSQL("603_admin_coupons_write_ui_i18n.sql"),
	})
}
