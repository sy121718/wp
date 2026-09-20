package migrations

import "sync"

// register_product_related_i18n.go — 商品「相关商品」引用校验的词条（迁移 304，审计 DB-03 / PROD-01）。
//
// 与 239（商品类型）/ 298（标签命中商品）同类的「新增 enums 常量必须同批 seed」：
// product 的 key 就是 sys_i18n 的 item_key，漏 seed 不会让任何测试失败，
// 只会让英文界面回落中文兜底。
//
// 注册方式：由 register.go 的 init() 显式调用（与 294 / 298 同形）。
func registerProductRelatedIDsI18n() {
	registerProductRelatedIDsI18nOnce.Do(registerProductRelatedIDsI18nSeed)
}

// registerProductRelatedIDsI18nOnce 让重复调用成为空操作。
var registerProductRelatedIDsI18nOnce sync.Once

// registerProductRelatedIDsI18nSeed 注册 304（真正干活的那一半）。
func registerProductRelatedIDsI18nSeed() {
	// 304：1 个 key × 2 语言 = 2 行词条。
	//
	// 幂等判定把 key 写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换（TableName 只进台账），写成 item_key = ? 永远查不到行，
	// 这条 seed 就会每次启动重跑（178 踩过同形的坑）。门槛取本 key 的 zh-CN 行数。
	registerSeed(Seed{
		Version:   "304-i18n-seed-product-related-ids",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('ErrRelatedInvalid')",
		SQL: mustSQL("304_i18n_seed_product_related_ids.sql"),
	})
}
