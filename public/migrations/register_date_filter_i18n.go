package migrations

import "sync"

// register_date_filter_i18n.go — 后台统一时间筛选条的词条（584）。
//
// 门槛取本批自己的代表 key：admin.common.filter.preset 只可能由这一批种下，
// 它存在即本批已跑过。不要用 admin.common.filter.submit / reset 计数 ——
// 那两个是更早的迁移种的，存量库永远满足，本条就永远不会执行。
func registerDateFilterI18n() {
	registerDateFilterI18nOnce.Do(registerDateFilterI18nSeed)
}

var registerDateFilterI18nOnce sync.Once

func registerDateFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "584-date-filter-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("584_date_filter_i18n.sql"),
		// 1 个代表 key × 2 语言 = 2 行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.common.filter.preset'",
	})
}
