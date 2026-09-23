package migrations

import "sync"

// register_blocks_one_table_i18n.go — 全局块管理页「一张表 + 影响面降级」的新增词条（421）。
//
// 见 421_i18n_blocks_one_table.sql 的头部：blocks.html 把三段表合并成一张表（02-L P1-4），
// 并把「待重建影响面」从常驻只读卡降级为页头徽章 + 折叠清单（02-L P1-10）。
// 两处改动各自带来一条没有现成词条可复用的文案：页头徽章的计数后缀、徽章旁的解释正文；
// 合并后的空态也从「每段一条」变成唯一一条。
//
// 本批新增 3 个 key × 2 语言 = 6 行：
//
//	admin.blocks.impact.badgeTail
//	admin.blocks.impact.help
//	admin.blocks.list_empty
//
// 三条被取代的旧词条（headers_empty / footers_empty / blocks_empty）**不在此处退役**：
// 它们由 192 seed，而 register_admin_i18n.go 的幂等条件是逐条枚举这些 key 计数的 ——
// 只删词条会让那条 seed 每次启动把它们插回来。完整的退役要连同 192 的 SQL 与那个条件
// 一起改（属专门的孤儿词条清理批次），不在本批范围内。
//
// 注册方式：本文件自带 init()（与 register_content_list_filter_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerBlocksOneTableI18n() {
	registerBlocksOneTableI18nOnce.Do(registerBlocksOneTableI18nSeed)
}

// registerBlocksOneTableI18nOnce 让重复调用成为空操作。
var registerBlocksOneTableI18nOnce sync.Once

// registerBlocksOneTableI18nSeed 注册 421（真正干活的那一半）。
func registerBlocksOneTableI18nSeed() {
	// 门槛 = 本批 3 个 key 的 **en-US** 行都在（= 3 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与模板里的兜底文案同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过（413 同形）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行，让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "421-i18n-blocks-one-table",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.blocks.impact.badgeTail','admin.blocks.impact.help','admin.blocks.list_empty')",
		SQL: mustSQL("421_i18n_blocks_one_table.sql"),
	})
}

func init() { registerBlocksOneTableI18n() }
