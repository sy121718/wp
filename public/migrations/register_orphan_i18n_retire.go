package migrations

import "sync"

// register_orphan_i18n_retire.go — 退役孤儿词条（419）。
//
// 见 419_retire_orphan_i18n.sql 的头部：7 个 key 在模板与代码里都没有引用者
// （商品分类 / 品牌的 SEO 空值占位改成硬编码「—」、库存三页的假分页条换成真源分页、
// 邮件活动页的假分页条换成真分页条）。
//
// 与三处 seed 的关系（AGENTS.md「删能力时要连 seed 的 SQL 与幂等条件一起收口」）：
//
//	· 190 —— 幂等条件逐条枚举本批 key（含 campaign 的 2 个），本批把 2 个 key 移出列表、
//	  门槛 764 → 762，并删掉 seed SQL 里那 4 行（register_admin_i18n.go 同批改）。
//	· 230 —— 幂等条件取 3 个代表 key（不含 seo.unset），删除不影响它；只删 seed SQL 里的 4 行。
//	· 412 —— 条件恰好由这 3 个 key 构成，整批退役（seed 注册与 SQL 一并删除）。
//
// 注册方式：本文件自带 init()（与 register_customers_empty_retire.go 同形）。
func registerOrphanI18nRetire() {
	registerOrphanI18nRetireOnce.Do(registerOrphanI18nRetireSeed)
}

// registerOrphanI18nRetireOnce 让重复调用成为空操作。
var registerOrphanI18nRetireOnce sync.Once

// registerOrphanI18nRetireSeed 注册 419（真正干活的那一半）。
func registerOrphanI18nRetireSeed() {
	// 门槛 = 这 7 个 key 一行都不剩（本批已跑完）。
	// 正常形态是「本条件不成立 → 执行删除」：190 若因故重跑会把 campaign 的 4 行插回来，
	// 那时本条正好再删一次；只有在本条已跑完、相关 seed 又都没重跑的同一次 RunSeeds 里
	// 读到才会命中（那也是对的：没有要删的行）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换：key 只能写在 SQL 字面量里（178 踩过）。
	registerSeed(Seed{
		Version:   "419-i18n-retire-orphans",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.product_categories.seo.unset','admin.product_brands.seo.unset'," +
			"'admin.inventory.pagination.info','admin.inventory.pagination.more','admin.inventory.pagination.end'," +
			"'admin.mail.campaign.page_prefix','admin.mail.campaign.page_suffix')",
		SQL: mustSQL("419_retire_orphan_i18n.sql"),
	})
}

func init() { registerOrphanI18nRetire() }
