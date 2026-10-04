package migrations

import "sync"

// register_customer_rfm_i18n.go — RFM 分析页的词条（560）。
//
// 判据按本批自己的对象枚举（上界封闭）：挑两个代表 key（一个表头 + 一个分段名），
// 各自的中英两条都必须在（26 键 × 2 语言 = 52 条元组，代表 key 语言齐全即认完成）。
//
// 代表的取法避开页头说明那几条：它们是**多个 key 拼一句**（中间夹 <strong>），
// 单独挑一个看不出这条句子是否完整。表头与分段名各自自成一个词条。
func registerCustomerRfmI18n() {
	registerCustomerRfmI18nOnce.Do(registerCustomerRfmI18nSeed)
}

// registerCustomerRfmI18nOnce 让重复调用成为空操作。
var registerCustomerRfmI18nOnce sync.Once

// registerCustomerRfmI18nSeed 注册 560（真正干活的那一半）。
func registerCustomerRfmI18nSeed() {
	registerSeed(Seed{
		Version:   "560-customer-rfm-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.rfm.col.segment', 'admin.customer.rfm.segment.vip') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("560_customer_rfm_i18n.sql"),
	})
}
