package migrations

import "sync"

// register_product_category_parent_i18n.go — 分类列表行「父级不属于本工程」徽章的词条（seed 508）。
//
// 508：`admin.product_categories.parent.out_of_scope` 的中英两行。它同时被
// product_categories.html 的列表行徽章与 product_taxonomy_page.go 的 categoryParentText
// （抽屉里的父级下拉）消费 —— 同一个 key 两处取词，所以只 seed 一次。
//
// 门槛判据（ConditionSQL）**逐条枚举本批自己的 key**，不用 LIKE 前缀、不用全库总量：
// 主键是 (item_key, lang)，同一 key 同一语言至多一行，于是
// 「COUNT(DISTINCT item_key) >= 1 且 COUNT(*) >= 2」严格等价于「这一个 key 的中英两行都在」。
// 偏差方向刻意选「宁可重跑，不可静默跳过」：被运维删掉一行 → 条件不成立 → 下次启动补回
// （AGENTS.md 记了两个反方向的真实故障：058 前缀虚高导致本批被跳过、076 计数永远追不平导致每次重跑）。
//
// 注册方式：本文件**自带 func init() 自注册**，不在 register.go 的 init() 里再挂一处 ——
// register.go 是所有人加迁移都会碰的热点文件，自注册让「谁负责注册」仍只有一个真源
// （只有这里这一处调用，sync.Once 兜住重复）。505 与 399 是同一种写法。
func init() { registerProductCategoryParentI18n() }

// registerProductCategoryParentI18n 注册 508。
func registerProductCategoryParentI18n() {
	registerProductCategoryParentI18nOnce.Do(registerProductCategoryParentI18nSeed)
}

// registerProductCategoryParentI18nOnce 让重复调用成为空操作。
var registerProductCategoryParentI18nOnce sync.Once

// registerProductCategoryParentI18nSeed 注册 508（真正干活的那一半）。
func registerProductCategoryParentI18nSeed() {
	registerSeed(Seed{
		Version:   "508-i18n-category-parent-out-of-scope",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 1 AND COUNT(*) >= 2 THEN 1 ELSE 0 END " +
			"FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN (" +
			"'admin.product_categories.parent.out_of_scope')",
		SQL: mustSQL("508_product_category_parent_i18n.sql"),
	})
}
