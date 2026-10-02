package migrations

import "sync"

// register_locale_admission_i18n.go — 语言准入（U1）与缺译报告（U2）的词条（492）。
//
// 注册方式：由 register.go 的 init() 显式调用。
func registerLocaleAdmissionI18n() {
	registerLocaleAdmissionI18nOnce.Do(registerLocaleAdmissionI18nSeed)
}

// registerLocaleAdmissionI18nOnce 让重复调用成为空操作。
var registerLocaleAdmissionI18nOnce sync.Once

// registerLocaleAdmissionI18nSeed 注册 492（真正干活的那一半）。
func registerLocaleAdmissionI18nSeed() {
	// 492：13 个 key × 2 语言。
	//
	// 门槛判据按**本批自己的 key 逐条枚举**（写进 SQL 字面量：ConditionSQL 由迁移器
	// db.Raw 直接执行、没有参数替换，用 `?` 永远查不到行 → 每次启动重跑，178 踩过）。
	// 上界封闭：不用 LIKE 前缀、不用全库总量。
	registerSeed(Seed{
		Version:   "492-i18n-locale-admission-miss-report",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'ErrLocaleNoTranslations'," +
			"'admin.page.translation_misses.title'," +
			"'admin.page.translation_misses.back'," +
			"'admin.page.translation_misses.hint'," +
			"'admin.page.translation_misses.col.page'," +
			"'admin.page.translation_misses.col.lang'," +
			"'admin.page.translation_misses.col.misses'," +
			"'admin.page.translation_misses.col.actions'," +
			"'admin.page.translation_misses.empty'," +
			"'admin.page.translation_misses.unit.misses'," +
			"'admin.page.translation_misses.candidates'," +
			"'admin.page.translation_misses.action.cancel'," +
			"'admin.page.translation_misses.canceled'" +
			")",
		SQL: mustSQL("492_locale_admission_and_miss_report_i18n.sql"),
	})
}
