package migrations

// register_shipping_settings_i18n.go — 迁移 465 的 seed 注册（站点级运费设置的文案词条）。
//
// 门槛判据**枚举本批自己的 9 个 item_key**（上界封闭，9 × 2 语言 = 18 行）：
//
//	· 用 LIKE 前缀（`admin.settings.%`）会让「别的批次已有同前缀行」把计数抬高，
//	  本批被静默跳过 —— 058 的真实故障；
//	· 用全库总量会在将来新增同前缀 key 时永远追不平，每次启动都重跑 —— 076 的真实故障。
//
// 注册方式：本文件自带 func init()（与 455 / 462a / 464 一致），不在 register.go 的 init()
// 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "465-shipping-settings-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'ErrShippingBaseFeeInvalid', 'ErrShippingFreeThresholdInvalid', " +
			"'admin.settings.section.shipping', 'admin.settings.field.shipping_base_fee', " +
			"'admin.settings.ph.shipping_base_fee', 'admin.settings.hint.shipping', " +
			"'admin.settings.field.shipping_free_threshold', " +
			"'admin.settings.ph.shipping_free_threshold', " +
			"'admin.settings.hint.shipping_threshold'" +
			")",
		SQL: mustSQL("465_shipping_settings_i18n.sql"),
	})
}
