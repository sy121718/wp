package migrations

// 459 — page / mail 两模块业务错误**补充说明**的词条（i18n.ErrorDetail 协议）。
//
// 背景与形态见 459_i18n_page_mail_err_detail.sql 头部；读侧改造见
// internal/module/page/inbound/http/page_err.go（pageDetailText）与
// internal/module/mail/inbound/http/mail_err.go（mailDetailText）。
//
// 门槛判据**逐条枚举本批自己的 item_key**（上界封闭），不用 LIKE 前缀、也不用全库总量 ——
// 依据 AGENTS.md §数据库·迁移 的两条判据与 058 / 076 两次真实故障：
// 前缀判据在「已有别的批次同前缀行」时计数虚高 → 本批被静默跳过；判据用全库总量时
// 将来新增同前缀 key 永远追不平 → 每次启动重跑。
//
// 行数 = key 数 × 2（每个 key 中英各一行）：22 × 2 = 44。
//
// 放在**种子**台账（registerSeed 而不是 register）：本批只新增词条、不改任何既有 key，
// 词条属于 seed 语义（可重复写入的默认值），与其它 i18n 批次无先后约束。
//
// 注册方式：本文件自带 init()（与 register_product_inventory_go_texts_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "459-i18n-page-mail-err-detail",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 44 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			// page：页面文档校验明细（5 个 key）。
			"'admin.page.detail.documentEmpty', 'admin.page.detail.nodeDepthExceed', " +
			"'admin.page.detail.nodeInvalid', 'admin.page.detail.settingsInvalid', " +
			"'admin.page.detail.structureInvalid', " +
			// mail：流程图校验明细（17 个 key）。
			"'admin.mail.detail.graphEmpty', 'admin.mail.detail.requestNotJSON', " +
			"'admin.mail.detail.notGraph', 'admin.mail.detail.noNode', " +
			"'admin.mail.detail.nodeKeyMissing', 'admin.mail.detail.nodeKeyDuplicate', " +
			"'admin.mail.detail.needMinutes', 'admin.mail.detail.needTemplate', " +
			"'admin.mail.detail.needTwoArms', 'admin.mail.detail.needCondition', " +
			"'admin.mail.detail.needTagAction', 'admin.mail.detail.unknownNodeType', " +
			"'admin.mail.detail.entryMissing', 'admin.mail.detail.entryNotExist', " +
			"'admin.mail.detail.edgeTargetMissing', 'admin.mail.detail.cycle', " +
			"'admin.mail.detail.unreachable'" +
			") AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("459_i18n_page_mail_err_detail.sql"),
	})
}
