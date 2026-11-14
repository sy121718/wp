package migrations

import "sync"

func registerProductTitleRetiredKeyCleanup() {
	productTitleRetiredKeyCleanupOnce.Do(registerProductTitleRetiredKeyCleanupSeed)
}

var productTitleRetiredKeyCleanupOnce sync.Once

// 758 清理 735 已退役的四组页面标题 key 的单侧残留。
//
// 058 的 SQL 仍按历史原文播种这四行，且它的幂等守门把四组语言对算作未完成；
// 058 的原文是哈希归档不能回改，故在 735 之后用本 seed 做终态清理。
func registerProductTitleRetiredKeyCleanupSeed() {
	registerSeed(Seed{
		Version:   "758-product-title-retired-key-cleanup",
		TableName: "sys_i18n",
		// 四组旧 key 一行不剩时返回 1（已清理，跳过）；还有残留时返回 0（执行删除）。
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('MsgAdministratorsTitle', 'zh-CN'), ('MsgAdministratorsTitle', 'en-US'),
  ('MsgDatarulesTitle', 'zh-CN'), ('MsgDatarulesTitle', 'en-US'),
  ('MsgDepartmentsTitle', 'zh-CN'), ('MsgDepartmentsTitle', 'en-US'),
  ('MsgPermissionsTitle', 'zh-CN'), ('MsgPermissionsTitle', 'en-US'))`,
		SQL: mustSQL("758_product_title_retired_key_cleanup.sql"),
	})
}
