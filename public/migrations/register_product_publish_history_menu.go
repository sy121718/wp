package migrations

import "sync"

func init() { registerProductPublishHistoryMenu() }

var productPublishHistoryMenuOnce sync.Once

func registerProductPublishHistoryMenu() {
	productPublishHistoryMenuOnce.Do(func() {
		registerSeed(Seed{
			Version:      "673c-product-publish-history-menu",
			TableName:    "sys_menus",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title_key = 'product.publish_attempts' AND type = 3 AND deleted_at IS NULL",
			SQL:          mustSQL("673c_product_publish_history_menu.sql"),
		})
	})
}
