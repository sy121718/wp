package migrations

import "sync"

// register_customer_rfm_menu.go — 客户目录的「RFM 分析」菜单（559）。
//
// **必须是 registerSeed 不是 register**：Migration 的默认存在性检查只看 TableName
// 那张表在不在，sys_menus 早就在 → 会直接跳过；Seed.ConditionSQL 才问得出
// 「这一行在不在」。
//
// 判据按本批自己的对象枚举（上界封闭）：只看 path = '/admin/customers/rfm' 这一行，
// 且必须 type = 2（菜单项）—— 不带 type 的话，一个同 path 的一级分组也算命中。
func registerCustomerRfmMenu() {
	registerCustomerRfmMenuOnce.Do(registerCustomerRfmMenuSeed)
}

// registerCustomerRfmMenuOnce 让重复调用成为空操作。
var registerCustomerRfmMenuOnce sync.Once

// registerCustomerRfmMenuSeed 注册 559（真正干活的那一半）。
func registerCustomerRfmMenuSeed() {
	registerSeed(Seed{
		Version:   "559-customer-rfm-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE type = 2 AND path = '/admin/customers/rfm') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("559_customer_rfm_menu.sql"),
	})
}
