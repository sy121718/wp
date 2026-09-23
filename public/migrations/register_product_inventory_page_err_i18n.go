package migrations

import "sync"

// register_product_inventory_page_err_i18n.go — 商品 / 库存域后台页失败出口的新增词条（407）。
//
// 见 407_i18n_product_inventory_page_err.sql 的头部：三处页面 handler 的失败出口
// （预览入口的硬编码中文常量、库存两页的裸归口 key）本批收成「?err= 回来源页」与
// 「降级渲染」，文案随之走 i18n。
//
// 本批新增 3 个 key（admin.product_detail_template.depsMissing、
// admin.common.list.loadFailedTitle / .loadFailedDesc）并补 MsgInternalError 的 en-US，
// 共 7 行；不修改任何既有词条的值。
//
// 注册方式：本文件自带 init()（与 register_product_inventory_empty_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerProductInventoryPageErrI18n() {
	registerProductInventoryPageErrI18nOnce.Do(registerProductInventoryPageErrI18nSeed)
}

// registerProductInventoryPageErrI18nOnce 让重复调用成为空操作。
var registerProductInventoryPageErrI18nOnce sync.Once

// registerProductInventoryPageErrI18nSeed 注册 407（真正干活的那一半）。
func registerProductInventoryPageErrI18nSeed() {
	// 门槛 = 本批 4 个 key 的 zh-CN 行都在（说明本批已落库，无需再插）。
	//
	// 为什么按 zh-CN 计数而不是连 en-US 一起数：MsgInternalError 的 zh-CN 行早在 058 就存在，
	// 而它的 en-US 是本批补的 —— 若把 en-US 也算进门槛，另一个批次先补了同一行时本条件
	// 仍会满足（无误），但「条件命中」这件事就不再对应「本批已落库」了。按 zh-CN 计数时，
	// 另外 3 个 key 的 zh-CN 只可能由本批写入，判据与本批一一对应。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "407-i18n-product-inventory-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.product_detail_template.depsMissing'," +
			"'admin.common.list.loadFailedTitle','admin.common.list.loadFailedDesc'," +
			"'MsgInternalError')",
		SQL: mustSQL("407_i18n_product_inventory_page_err.sql"),
	})
}

func init() { registerProductInventoryPageErrI18n() }
