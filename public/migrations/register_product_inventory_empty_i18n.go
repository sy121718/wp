package migrations

import "sync"

// register_product_inventory_empty_i18n.go — 商品 / 库存域空态收口的新增词条（402）。
//
// 见 402_i18n_product_inventory_empty.sql 的头部：模板里的中文只是 t() 兜底，词条命中时
// 显示的是库里的值；191 的 seed 是 ON CONFLICT DO NOTHING，改模板文案对存量库是 no-op，
// 所以 D8 / D9 / D10 的三处文案修正只能由一条新迁移完成。
//
// 本批只**新增** 7 个 key（2 语言共 14 行），不修改任何既有词条：
//   admin.inventory.moves.emptyLead / .emptyPurchaseLink / .emptyTail   —— 流水空态拆三段拼链接
//   admin.inventory.list.emptyNew                                       —— 仓库空态（原句指向不存在的表单）
//   admin.inventory_sources.list.emptyTitle / .emptyNew / .emptyFiltered —— 货源空态分两档
//
// 注册方式：本文件自带 init()（与 register_list_empty_i18n.go / register_showcase_blueprints.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerProductInventoryEmptyI18n() {
	registerProductInventoryEmptyI18nOnce.Do(registerProductInventoryEmptyI18nSeed)
}

// registerProductInventoryEmptyI18nOnce 让重复调用成为空操作。
var registerProductInventoryEmptyI18nOnce sync.Once

// registerProductInventoryEmptyI18nSeed 注册 402（真正干活的那一半）。
func registerProductInventoryEmptyI18nSeed() {
	// 门槛 = 7 个 key 的 zh-CN 行都在（说明本批已落库，无需再插）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "402-i18n-product-inventory-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.inventory.moves.emptyLead','admin.inventory.moves.emptyPurchaseLink'," +
			"'admin.inventory.moves.emptyTail','admin.inventory.list.emptyNew'," +
			"'admin.inventory_sources.list.emptyTitle','admin.inventory_sources.list.emptyNew'," +
			"'admin.inventory_sources.list.emptyFiltered')",
		SQL: mustSQL("402_i18n_product_inventory_empty.sql"),
	})
}

func init() { registerProductInventoryEmptyI18n() }
