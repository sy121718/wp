package migrations

import "sync"

// register_customer_segment_filter_i18n.go — 客户列表「消费分段」筛选的词条（558）。
//
// 判据：两个代表 key（一个标签 + 一个选项）的中英四条必须都在（8 键 × 2 语言 = 16 条元组）。
func registerCustomerSegmentFilterI18n() {
	registerCustomerSegmentFilterI18nOnce.Do(registerCustomerSegmentFilterI18nSeed)
}

// registerCustomerSegmentFilterI18nOnce 让重复调用成为空操作。
var registerCustomerSegmentFilterI18nOnce sync.Once

// registerCustomerSegmentFilterI18nSeed 注册 558（真正干活的那一半）。
func registerCustomerSegmentFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "558-customer-segment-filter-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customers.field.segment', 'admin.customers.segment.repurchasing') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("558_customer_segment_filter_i18n.sql"),
	})
}
