package migrations

// register_product_publish_records_menu.go — 662 的注册项（B9 发布记录页的菜单）。
//
// 判据判的是「那一行在」：path 命中且未软删。不用「菜单表非空即跳过」——
// 存量库菜单表恒非空，那样写这条种子永远不会跑（648 的同一理由）。

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
