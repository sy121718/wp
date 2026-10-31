package migrations

import "sync"

func registerRetireSeoGradeKeys() {
	registerRetireSeoGradeKeysOnce.Do(registerRetireSeoGradeKeysSeed)
}

var registerRetireSeoGradeKeysOnce sync.Once

func registerRetireSeoGradeKeysSeed() {

	registerSeed(Seed{
		Version:   "458-i18n-retire-seo-grade-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.seo.grade.green','admin.seo.grade.lightgreen','admin.seo.grade.yellow'," +
			"'admin.seo.grade.red','admin.seo.grade.redBlocking')",
		SQL: mustSQL("458_retire_seo_grade_keys.sql"),
	})
}

func init() { registerRetireSeoGradeKeys() }
