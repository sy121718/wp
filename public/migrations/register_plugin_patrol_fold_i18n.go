package migrations

import "sync"

func registerPluginPatrolFoldI18n() {
	registerPluginPatrolFoldI18nOnce.Do(registerPluginPatrolFoldI18nSeed)
}

var registerPluginPatrolFoldI18nOnce sync.Once

func registerPluginPatrolFoldI18nSeed() {

	registerSeed(Seed{
		Version:   "426-plugin-patrol-fold-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.plugins.patrol.title','admin.plugins.patrol.badge_kinds')",
		SQL: mustSQL("426_plugin_patrol_fold_i18n.sql"),
	})
}

func init() { registerPluginPatrolFoldI18n() }
