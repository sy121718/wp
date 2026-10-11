package migrations

import "sync"

var registerAdminCustomerWriteUII18nOnce sync.Once

func registerAdminCustomerWriteUII18n() {
	registerAdminCustomerWriteUII18nOnce.Do(func() {
		registerSeed(Seed{Version: "610-admin-customer-write-ui-i18n", TableName: "sys_i18n", ConditionSQL: "SELECT CASE WHEN COUNT(*) = 16 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('admin.customers.write.noneSelected', 'zh-CN'), ('admin.customers.write.noneSelected', 'en-US'), ('admin.customers.write.allUpdated', 'zh-CN'), ('admin.customers.write.allUpdated', 'en-US'), ('admin.customers.write.allSkipped', 'zh-CN'), ('admin.customers.write.allSkipped', 'en-US'), ('admin.customers.write.partial', 'zh-CN'), ('admin.customers.write.partial', 'en-US'), ('admin.customers.write.refresh', 'zh-CN'), ('admin.customers.write.refresh', 'en-US'), ('admin.customers.write.failed', 'zh-CN'), ('admin.customers.write.failed', 'en-US'), ('admin.customers.write.noJs', 'zh-CN'), ('admin.customers.write.noJs', 'en-US'), ('user.msg.customerBulkUnlock', 'zh-CN'), ('user.msg.customerBulkUnlock', 'en-US'))", SQL: mustSQL("610_admin_customer_write_ui_i18n.sql")})
	})
}
