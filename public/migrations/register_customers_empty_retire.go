package migrations

import "sync"

// register_customers_empty_retire.go — 退役孤儿词条 admin.customers.empty（418）。
//
// 见 418_retire_customers_empty_i18n.sql 的头部：
//
//	· 模板已不再引用这个 key（现用 admin.customers.list.empty_heading / .empty_desc）；
//	· 它的 seed 在 190 批次里，而 190 的幂等条件把本 key 算进计数（>= 765）——
//	  只删行会让 190 下次启动重灌，所以删除必须排到 190 **之后**（seed 台账按版本排序）。
//	  这是 AGENTS.md「删能力要连 seed 与幂等条件一起收口」的已知缺口的一半：
//	  另一半（把 190 的门槛 765 降到 764 并移除该 key）在 register_admin_i18n.go 里，
//	  本批未动那个文件（见 SQL 头部的「遗留的一半」）。
//
// 注册方式：本文件自带 init()（与 register_order_page_err_i18n.go 同形）。
func registerCustomersEmptyRetire() {
	registerCustomersEmptyRetireOnce.Do(registerCustomersEmptyRetireSeed)
}

// registerCustomersEmptyRetireOnce 让重复调用成为空操作。
var registerCustomersEmptyRetireOnce sync.Once

// registerCustomersEmptyRetireSeed 注册 418（真正干活的那一半）。
func registerCustomersEmptyRetireSeed() {
	// 门槛 = 该 key 一行都不剩。190 每轮会把它的两行插回来（ON CONFLICT DO NOTHING），
	// 所以正常形态是「本条件不成立 → 执行删除」；只有在本条已经跑完、190 又没重跑的同一次
	// RunSeeds 里读过才会命中（那也是对的：没有要删的行）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换：key 只能写在 SQL 字面量里（178 踩过）。
	registerSeed(Seed{
		Version:      "418-i18n-retire-customers-empty",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'admin.customers.empty'",
		SQL:          mustSQL("418_retire_customers_empty_i18n.sql"),
	})
}

func init() { registerCustomersEmptyRetire() }
