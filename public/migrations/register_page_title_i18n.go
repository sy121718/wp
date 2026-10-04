package migrations

import "sync"

// register_page_title_i18n.go — 后台整页标题词条（520）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：本批只新增一个 key
// （admin.system.title，sysconfig 系统设置页的 <title>），中英两条元组。
// 门槛用「代表 key 的语言齐全」——不枚举全库、不用 LIKE 前缀
// （存量库永远满足 → 补词条的那条迁移永远不会执行，迁移 494 记过这个坑）。
//
// AI 的两处标题复用既有 key（admin.ai.title 见 513、admin.ai.session.title 见 516），
// 所以不在这里重复 seed。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」：代表 key 齐全即视为整批已完成；
// ON CONFLICT DO NOTHING 保证重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerPageTitleI18n() {
	registerPageTitleI18nOnce.Do(registerPageTitleI18nSeed)
}

// registerPageTitleI18nOnce 让重复调用成为空操作。
var registerPageTitleI18nOnce sync.Once

// registerPageTitleI18nSeed 注册 520（真正干活的那一半）。
func registerPageTitleI18nSeed() {
	registerSeed(Seed{
		Version:   "520-page-title-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.system.title') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("520_page_title_i18n.sql"),
	})
}
