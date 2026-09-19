package migrations

import "sync"

// register_product_tag_hits_i18n.go — 商品标签页「命中商品」展开区的词条（迁移 298，审计 PERF-02）。
//
// 与 228 / 229 / 230 / 283 同类的「模板新增文案位」补词条：模板兜底只在词条缺失时显示，
// 漏一条 en-US 不会有任何报错，只会让英文界面回落中文。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册，与 294 同形）。
func registerProductTagHitsI18n() {
	registerProductTagHitsI18nOnce.Do(registerProductTagHitsI18nSeed)
}

// registerProductTagHitsI18nOnce 让重复调用成为空操作。
var registerProductTagHitsI18nOnce sync.Once

// registerProductTagHitsI18nSeed 注册 298（真正干活的那一半）。
func registerProductTagHitsI18nSeed() {
	// 298：2 个 key × 2 语言 = 4 行词条。
	//
	// 幂等判定把 key 写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换（TableName 只进台账），写成 item_key = ? 永远查不到行，
	// 这条 seed 就会每次启动重跑（178 踩过同形的坑）。
	// 门槛取本批 2 个 key 的 zh-CN 行数，不用全库计数 —— 存量库永远满足，补词条就永远不会执行。
	registerSeed(Seed{
		Version:   "298-i18n-seed-product-tag-hits",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.product_tags.list.hitHint','admin.product_tags.hits.error')",
		SQL: mustSQL("298_i18n_seed_product_tag_hits.sql"),
	})
}
