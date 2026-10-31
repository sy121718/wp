package migrations

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
