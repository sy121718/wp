package migrations

import "sync"

func registerProjectPageJumpI18n() {
	registerProjectPageJumpI18nOnce.Do(registerProjectPageJumpI18nSeed)
}

var registerProjectPageJumpI18nOnce sync.Once

func registerProjectPageJumpI18nSeed() {

	registerSeed(Seed{
		Version:   "590-project-page-jump-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.theme.ok.created','admin.theme.ok.activated','admin.theme.ok.deleted')",
		SQL: mustSQL("590_project_page_jump_i18n.sql"),
	})
}

func init() { registerProjectPageJumpI18n() }
