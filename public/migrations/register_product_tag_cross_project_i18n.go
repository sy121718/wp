package migrations

import "sync"

// register_product_tag_cross_project_i18n.go — 商品标签跨工程引用拒绝的词条（迁移 312，审计 DB-03 / PROD-02）。
//
// 与 239（商品类型）/ 298（标签命中商品）/ 304（相关商品引用）同类的「新增 enums 常量必须同批 seed」：
// product 的 key 就是 sys_i18n 的 item_key，漏 seed 不会让任何测试失败，
// 只会让英文界面回落中文兜底。
//
// 注册方式：由 register.go 的 init() 显式调用（与 294 / 298 / 304 同形）。
func registerProductTagCrossProjectI18n() {
	registerProductTagCrossProjectI18nOnce.Do(registerProductTagCrossProjectI18nSeed)
}

// registerProductTagCrossProjectI18nOnce 让重复调用成为空操作。
var registerProductTagCrossProjectI18nOnce sync.Once

// registerProductTagCrossProjectI18nSeed 注册 312（真正干活的那一半）。
func registerProductTagCrossProjectI18nSeed() {
	// 312：1 个 key × 2 语言 = 2 行词条。
	//
	// 幂等判定把 key 写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换（TableName 只进台账），写成 item_key = ? 永远查不到行，
	// 这条 seed 就会每次启动重跑（178 踩过同形的坑）。门槛取本 key 的 zh-CN 行数。
	registerSeed(Seed{
		Version:   "312-i18n-seed-product-tag-cross-project",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('ErrTagCrossProject')",
		SQL: mustSQL("312_i18n_seed_product_tag_cross_project.sql"),
	})
}
