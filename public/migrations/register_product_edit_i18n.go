package migrations

import "sync"

// register_product_edit_i18n.go — 商品列表筛选分页 + 商品编辑页的词条（迁移 314）。
//
// 与 233（商品详情拆页）/ 298（标签命中商品）/ 304（相关商品校验）同类的
// 「模板新增文案位必须同批 seed」：模板兜底只在词条缺失时显示中文，
// 漏一条 en-US 不会有任何报错，只会让英文界面回落中文。
//
// 注册方式：由 register.go 的 init() 显式调用（与 294 / 298 / 304 同形）。
func registerProductEditI18n() {
	registerProductEditI18nOnce.Do(registerProductEditI18nSeed)
}

// registerProductEditI18nOnce 让重复调用成为空操作。
var registerProductEditI18nOnce sync.Once

// registerProductEditI18nSeed 注册 314（真正干活的那一半）。
func registerProductEditI18nSeed() {
	// 314：34 个 key × 2 语言 = 68 行词条。
	//
	// 幂等判定把 key 写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换（TableName 只进台账），写成 item_key = ? 永远查不到行，
	// 这条 seed 就会每次启动重跑（178 踩过同形的坑）。
	// 门槛取本批 3 个代表 key 的 zh-CN 行数，不用全库计数 —— 存量库永远满足，补词条就永远不会执行。
	registerSeed(Seed{
		Version:   "314-i18n-seed-product-edit",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.products.row.edit','admin.products.row.preview','product.msg.saved')",
		SQL: mustSQL("314_i18n_seed_product_edit.sql"),
	})
}
