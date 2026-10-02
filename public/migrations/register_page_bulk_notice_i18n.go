package migrations

import "sync"

// register_page_bulk_notice_i18n.go — 页面批量操作受控回执的词条（510）。
//
// **自注册**：本文件用 func init() 把自己登记进 seed 台账，不需要改 register.go
// —— 多代理共用工作区时，改 register.go 是并发冲突的高发点（另一批的 509 也走同一条约定）。
func init() { registerPageBulkNoticeI18n() }

// registerPageBulkNoticeI18nOnce 让重复调用成为空操作。
var registerPageBulkNoticeI18nOnce sync.Once

// registerPageBulkNoticeI18n 注册 510（幂等：重复调用是空操作）。
func registerPageBulkNoticeI18n() {
	registerPageBulkNoticeI18nOnce.Do(registerPageBulkNoticeI18nSeed)
}

// registerPageBulkNoticeI18nSeed 真正干活的那一半。
func registerPageBulkNoticeI18nSeed() {
	// 510：4 个 key × 2 语言。门槛判据按**本批自己的 key 逐条枚举**（上界封闭）：
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
	registerSeed(Seed{
		Version:   "510-i18n-page-bulk-notice",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.pages.bulk.nothing','admin.pages.bulk.deleted'," +
			"'admin.pages.bulk.skipped','admin.pages.bulk.partial'" +
			")",
		SQL: mustSQL("510_page_bulk_notice_i18n.sql"),
	})
}
