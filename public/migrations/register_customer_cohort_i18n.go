package migrations

import "sync"

// register_customer_cohort_i18n.go — 群组留存页的词条（562）。
//
// 判据按本批自己的对象枚举（上界封闭）：挑两个代表 key（一个表头 + 一句页脚标签），
// 各自的中英两条都必须在（19 键 × 2 语言 = 38 条，代表 key 语言齐全即认完成）。
//
// 代表的取法避开页头说明那几条：它们是**多个 key 拼一句**（中间夹 <strong>），
// 单独挑一个看不出这条句子是否完整。
func registerCustomerCohortI18n() {
	registerCustomerCohortI18nOnce.Do(registerCustomerCohortI18nSeed)
}

// registerCustomerCohortI18nOnce 让重复调用成为空操作。
var registerCustomerCohortI18nOnce sync.Once

// registerCustomerCohortI18nSeed 注册 562（真正干活的那一半）。
func registerCustomerCohortI18nSeed() {
	registerSeed(Seed{
		Version:   "562-customer-cohort-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.cohort.col.cohort', 'admin.customer.cohort.totalCustomers') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("562_customer_cohort_i18n.sql"),
	})
}
