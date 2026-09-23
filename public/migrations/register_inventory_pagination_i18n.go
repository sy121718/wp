package migrations

import "sync"

// register_inventory_pagination_i18n.go — 库存域三个列表页分页条的新增词条（412）。
//
// 见 412_i18n_inventory_pagination.sql 的头部：库存流水 / 货源 / 采购入库三页此前是
// 「写死上限 + 没有分页条」（流水 50 / 货源 200 / 采购单 100，第 N+1 条静默消失），
// 本批接入项目既有的分页设施，片段里「上一页 / 下一页」复用 059 已登记的 shell.pagination.*，
// 只新增本域信息行的 3 个 key（admin.inventory.pagination.info / .more / .end），
// 共 6 行（中英各一行 × 3）；不修改任何既有词条的值。
//
// 注册方式：本文件自带 init()（与 register_product_inventory_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
//
// 2026-09（419 批）：这 3 个 key 已随「降级分页条」一起退役（库存三页换成真源分页，
// 信息行与翻页按钮统一走 shell.pagination.*），由迁移 419 删除。
// 本批的幂等条件随之从「本批 3 个 key 的 en-US 行都在（>=3）」改成「3 个 key 一行都不剩（=0）」：
// 条件成立（库里没有）就跳过，于是本批不再往库里插回已退役的词条，也不会因条件恒假每轮重跑
// —— 这正是 AGENTS.md「删能力要连 seed 的 SQL 与幂等条件一起收口」要求的形态（122 号迁移的坑）。
func registerInventoryPaginationI18n() {
	registerInventoryPaginationI18nOnce.Do(registerInventoryPaginationI18nSeed)
}

// registerInventoryPaginationI18nOnce 让重复调用成为空操作。
var registerInventoryPaginationI18nOnce sync.Once

// registerInventoryPaginationI18nSeed 注册 412（真正干活的那一半）。
func registerInventoryPaginationI18nSeed() {
	// 门槛 = 本批 3 个 key 一行都不剩（已由 419 退役）。
	//
	// 原判据是「本批 3 个 key 的 **en-US** 行都在（>=3）」—— 按 en-US 计数而不是按 zh-CN：
	// 这 3 个 key 的中英两侧都只可能由本批写入，而 en-US 是「英文页面会不会显示中文兜底」的
	// 唯一判据，少一行时页面照样能渲染（回退中文原文），只有在英文站点上才看得出来。
	// 词条退役后该判据不再成立（419 已把 6 行删净），故改为「都不在库里则跳过」。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "412-i18n-inventory-pagination",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.inventory.pagination.info'," +
			"'admin.inventory.pagination.more','admin.inventory.pagination.end')",
		SQL: mustSQL("412_i18n_inventory_pagination.sql"),
	})
}

func init() { registerInventoryPaginationI18n() }
