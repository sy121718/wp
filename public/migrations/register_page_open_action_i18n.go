package migrations

// register_page_open_action_i18n.go — 674：页面列表「打开页面」单入口的词条。
//
// 判据逐条枚举本批的 1 个 key，并要求中英两行都在：只数一种语言时另一种缺行仍判已完成，
// 那条缺失的词就永远补不上（AGENTS.md §数据库「seed 门槛必须逐条枚举本批对象」，迁移 494 的坑）。
// 不用前缀 LIKE：将来任何新增的 admin.pages.action.* 都会让门槛提前满足。

import "sync"

func init() { registerPageOpenActionI18n() }

var pageOpenActionI18nOnce sync.Once

func registerPageOpenActionI18n() {
	pageOpenActionI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "674-page-open-action-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n
				WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('admin.pages.action.open')`,
			SQL: mustSQL("674_page_open_action_i18n.sql"),
		})
	})
}
