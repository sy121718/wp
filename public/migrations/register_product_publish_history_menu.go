package migrations

// register_product_publish_history_menu.go — 672b 的注册项（B11 发布历史按钮）。
//
// 判据：sys_menus 里 title_key='product.publish_attempts' 的按钮行存在（COUNT >= 1）。
// 不用「菜单存在即跳过」的默认形态：这条 seed 的职责就是「那一行在」，缺了就该重跑。

import "sync"

func init() { registerProductPublishHistoryMenu() }

var productPublishHistoryMenuOnce sync.Once

func registerProductPublishHistoryMenu() {
	productPublishHistoryMenuOnce.Do(func() {
		registerSeed(Seed{
			Version:      "672b-product-publish-history-menu",
			TableName:    "sys_menus",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title_key = 'product.publish_attempts' AND type = 3 AND deleted_at IS NULL",
			SQL:          mustSQL("672b_product_publish_history_menu.sql"),
		})
	})
}
