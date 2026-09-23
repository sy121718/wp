package migrations

import "sync"

// register_sources_filter_title_retire.go — 退役孤儿词条（436）。
//
// 见 436_retire_sources_filter_title_i18n.sql 的头部：货源页筛选栏的零样式
// .fold-title 标签已删除（决策：筛选栏不需要自报家门的冗余标签，保留 .help 悬浮），
// admin.inventory_sources.filter.title 失去引用。
//
// 与 seed 的关系（AGENTS.md「删能力时要连 seed 的 SQL 与幂等条件一起收口」）：
// 唯一 seed 是 191 批（register_admin_i18n.go），幂等条件逐条枚举本批 key ——
// 本批已同批把该 key 移出列表、门槛 582 → 581，并删掉
// 191_i18n_seed_product_inventory.sql 里那两行 INSERT。
//
// 注册方式：本文件自带 init()（与 register_orphan_i18n_retire.go 同形）。
func registerSourcesFilterTitleRetire() {
	registerSourcesFilterTitleRetireOnce.Do(registerSourcesFilterTitleRetireSeed)
}

// registerSourcesFilterTitleRetireOnce 让重复调用成为空操作。
var registerSourcesFilterTitleRetireOnce sync.Once

// registerSourcesFilterTitleRetireSeed 注册 436（真正干活的那一半）。
func registerSourcesFilterTitleRetireSeed() {
	// 门槛 = 这 1 个 key 一行都不剩（本批已跑完）。
	// 正常形态是「本条件不成立 → 执行删除」：191 若因故重跑会把本 key 的 2 行插回来
	// （已收口后不会再），那时本条正好再删一次；只有在本条已跑完、相关 seed 又都没重跑的
	// 同一次 RunSeeds 里读到才会命中（那也是对的：没有要删的行）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换：key 只能写在 SQL 字面量里（178 踩过）。
	registerSeed(Seed{
		Version:   "436-i18n-retire-sources-filter-title",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = " +
			"'admin.inventory_sources.filter.title'",
		SQL: mustSQL("436_retire_sources_filter_title_i18n.sql"),
	})
}

func init() { registerSourcesFilterTitleRetire() }
