package migrations

import "sync"

func registerAdminOrdersWriteUII18n() {
	registerAdminOrdersWriteUII18nOnce.Do(registerAdminOrdersWriteUII18nSeed)
}

var registerAdminOrdersWriteUII18nOnce sync.Once

func registerAdminOrdersWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "604-admin-orders-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.orders.bulk.noneSelected', 'admin.orders.bulk.refresh', " +
			"'admin.orders.bulkCancel.allSkipped', 'admin.orders.bulkCancel.allUpdated', " +
			"'admin.orders.bulkCancel.partial', 'admin.orders.bulkStatus.allSkipped', " +
			"'admin.orders.bulkStatus.allUpdated', 'admin.orders.bulkStatus.partial', " +
			"'admin.orders.bulkStatusConfirm', 'admin.orders.write.noJs')",
		SQL: mustSQL("604_admin_orders_write_ui_i18n.sql"),
	})
}
