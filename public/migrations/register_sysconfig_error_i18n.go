package migrations

import "sync"

func registerSysconfigErrorI18n() {
	registerSysconfigErrorI18nOnce.Do(registerSysconfigErrorI18nSeed)
}

var registerSysconfigErrorI18nOnce sync.Once

// registerSysconfigErrorI18nSeed 补齐 sysconfig 三个业务错误词条
// （ErrVersionRequired / ErrGroupNotFound / ErrVersionConflict）。
// 这三个 key 在 enums 里只声明、不内嵌文案，词库缺词时 JSON 出口会回落到
// 「系统内部错误」，把可修复的业务提示说成系统故障。
func registerSysconfigErrorI18nSeed() {
	registerSeed(Seed{
		Version:   "757-sysconfig-error-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('ErrVersionRequired', 'zh-CN'), ('ErrVersionRequired', 'en-US'),
  ('ErrGroupNotFound', 'zh-CN'), ('ErrGroupNotFound', 'en-US'),
  ('ErrVersionConflict', 'zh-CN'), ('ErrVersionConflict', 'en-US'))`,
		SQL: mustSQL("757_sysconfig_error_i18n.sql"),
	})
}
