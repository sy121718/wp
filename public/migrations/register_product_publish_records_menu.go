package migrations

import "sync"

func init() { registerProductPublishRecordsMenu() }

var productPublishRecordsMenuOnce sync.Once

func registerProductPublishRecordsMenu() {
	productPublishRecordsMenuOnce.Do(func() {
		registerSeed(Seed{
			Version:      "662-product-publish-records-menu",
			TableName:    "sys_menus",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_menus WHERE deleted_at IS NULL AND type = 2 AND path = '/admin/products/publish-records'",
			SQL:          mustSQL("662_product_publish_records_menu.sql"),
		})
	})
}
