package migrations

import "sync"

func registerCustomerSegmentFilterI18n() {
	registerCustomerSegmentFilterI18nOnce.Do(registerCustomerSegmentFilterI18nSeed)
}

var registerCustomerSegmentFilterI18nOnce sync.Once

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
