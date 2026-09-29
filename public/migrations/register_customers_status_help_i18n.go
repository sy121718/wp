package migrations

import "sync"

// register_customers_status_help_i18n.go — 客户列表的词条（431）。
//
// 见 431_i18n_customers_status_help.sql 的头部，两组：
//
//	· 状态列解释 3 key —— customers.html 原先逐行插一整行 colspan 说明（待激活 / 已锁定 /
//	  失败次数未清零），文案是 Go 侧硬编码中文；本批把说明移到状态列表头的 .help 并 key 化。
//	· 筛选态空态 2 key —— `empty_filtered_heading` / `empty_filtered_desc` 在 sys_i18n 与
//	  全仓 seed 里都不存在（模板在用时取中文兜底），英文界面因此回落中文
//	  （docs/02-V-decision-brief.md §3「顺带发现」、§8 结论表第 3 行的方案 A）。
//
// 注册方式：本文件自带 init()（与 register_customers_empty_retire.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerCustomersStatusHelpI18n() {
	registerCustomersStatusHelpI18nOnce.Do(registerCustomersStatusHelpI18nSeed)
}

// registerCustomersStatusHelpI18nOnce 让重复调用成为空操作。
var registerCustomersStatusHelpI18nOnce sync.Once

// registerCustomersStatusHelpI18nSeed 注册 431（真正干活的那一半）。
func registerCustomersStatusHelpI18nSeed() {
	// 门槛 = 本批 5 个新 key 的 **en-US** 行都在（= 5 行）。挑 en-US 而不是 zh-CN：
	// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（416 的同一理由）。
	//
	// 放在**种子**台账：本批只新增词条、不改任何既有 key，与 190 / 232 等 seed 无先后约束，
	// 但词条属于 seed 语义（可重复写入的默认值），与 416 保持一致。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "431-i18n-customers-status-help",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.customers.status.help.pending','admin.customers.status.help.locked'," +
			"'admin.customers.status.help.failures'," +
			"'admin.customers.list.empty_filtered_heading','admin.customers.list.empty_filtered_desc')",
		SQL: mustSQL("431_i18n_customers_status_help.sql"),
	})
}

func init() { registerCustomersStatusHelpI18n() }
