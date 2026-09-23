package migrations

import "sync"

// register_inventory_moves_empty_i18n.go — 库存流水空态文案的修正迁移（317）。
//
// 为什么要一条**覆盖**迁移而不是改 191 的 seed：191 用 INSERT ... ON CONFLICT DO NOTHING
// 写入 admin.inventory.moves.empty，键已存在即跳过 —— 改 seed SQL 对存量库是 no-op，
// 页面继续显示旧文案（模板里的中文 fallback 根本不参与，词条命中就显示库里的值）。
// 与 316 / 254 同一手法：UPDATE 旧值，历史迁移保持原样。
//
// 缺陷内容：旧文案写着「先用上面的表单做一次入库」，但 /admin/inventory 没有任何入库表单
// （入库在采购入库页），用户照着这句话在页面上找不到出路。详见 SQL 头部注释。
//
// 注册方式：由 register.go 的 init() 显式调用（与 294 / 298 / 304 / 314 / 315 / 316 同形）。
func registerInventoryMovesEmptyI18n() {
	registerInventoryMovesEmptyI18nOnce.Do(registerInventoryMovesEmptyI18nSeed)
}

// registerInventoryMovesEmptyI18nOnce 让重复调用成为空操作。
var registerInventoryMovesEmptyI18nOnce sync.Once

// registerInventoryMovesEmptyI18nSeed 注册 317（真正干活的那一半）。
func registerInventoryMovesEmptyI18nSeed() {
	// 317：1 个 key × 2 语言。
	//
	// 幂等判定把 key 与目标译文写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换 —— 写成 item_key = ? 永远查不到行，这条迁移会每次启动重跑（178 踩过）。
	// 门槛 = 「两种语言的当前值都已是目标文案」，此时无需 UPDATE；任一语言还是旧值就执行。
	//
	// 注意 SQL 里的 UPDATE 带 `AND item_value = '<旧值>'` 前置条件：只修正「还是旧默认值」
	// 的行，运营在后台手工改过的词条不动（seed 是默认值来源、后台是真相来源）。
	registerSeed(Seed{
		Version:   "317-i18n-fix-inventory-moves-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.inventory.moves.empty' " +
			"AND ((lang = 'zh-CN' AND item_value = '还没有库存流水 —— 入库请从采购入库页收货，盘点 / 报损请走右上角的库存调整。') " +
			"OR (lang = 'en-US' AND item_value = 'No stock movements yet — receive stock on the Purchase Receiving page; for counts or write-offs use Stock Adjustment in the top right.'))",
		SQL: mustSQL("317_fix_inventory_moves_empty_i18n.sql"),
	})
}
