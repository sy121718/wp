package migrations

import "sync"

func registerEmptyActionsI18n() {
	registerEmptyActionsI18nOnce.Do(registerEmptyActionsI18nSeed)
}

var registerEmptyActionsI18nOnce sync.Once

func registerEmptyActionsI18nSeed() {

	registerSeed(Seed{
		Version:   "415-i18n-empty-actions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.roles.perm.empty.action'," +
			"'admin.seo.paths.empty.action'," +
			"'admin.analytics.no_project.refresh','admin.analytics.empty.action'," +
			"'admin.mail.automation_run.timeline.empty.action'," +
			"'admin.mail.campaign.empty.action'," +
			"'admin.redirect.empty.action','admin.redirect.pick_project.title','admin.redirect.pick_project.action'," +
			"'admin.mail.marketing.contacts.empty.clear')",
		SQL: mustSQL("415_i18n_empty_actions.sql"),
	})
}

func init() { registerEmptyActionsI18n() }
