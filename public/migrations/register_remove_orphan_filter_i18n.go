package migrations

import "sync"

// register_remove_orphan_filter_i18n.go — 清理五个失去引用的筛选条词条（585）。
//
// 本批只有 DELETE，没有新 key 可当「跑过了吗」的判据，所以门槛反过来取**目标态**：
// 这五个 key 一行都不剩即为已完成。删干净后条件恒真，重跑只会 DELETE 0 行 —— 幂等。
// 新库从没种过它们，条件一开始就成立，同样只会删 0 行。
//
// 不要用 COUNT(*) >= 0 这种写法：那永远为真，等于没有门槛，与「迁移 494 的形状」
// （条件恒真导致脚本每次启动都重跑）是同一类问题 —— 只是这次后果无害而已。
func registerRemoveOrphanFilterI18n() {
	registerRemoveOrphanFilterI18nOnce.Do(registerRemoveOrphanFilterI18nSeed)
}

var registerRemoveOrphanFilterI18nOnce sync.Once

func registerRemoveOrphanFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "585-remove-orphan-filter-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("585_remove_orphan_filter_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.analytics.range.from', 'admin.analytics.range.to', " +
			"'admin.analytics.range.submit', 'admin.analytics.range.last30', " +
			"'admin.masterdata.action.filter')",
	})
}
