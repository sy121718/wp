package migrations

import "sync"

// register_ai_i18n.go — AI 模块词条（513）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：挑三个**代表 key**（各自的中英两条都必须在），
// 本批共 88 个 key × 2 语言 = 176 条元组，用「代表 key 的语言齐全」当门槛 ——
// 不枚举全部 176 条（门槛 SQL 会长到不可读），也不用全库计数 / LIKE 前缀
// （存量库永远满足 → 补词条的那条迁移永远不会执行，迁移 494 记过这个坑）。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」：代表 key 齐全即视为整批已完成；
// 若将来只补了一部分，门槛不满足 → 整条重跑（ON CONFLICT DO NOTHING 保证幂等）。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAII18n() {
	registerAII18nOnce.Do(registerAII18nSeed)
}

// registerAII18nOnce 让重复调用成为空操作。
var registerAII18nOnce sync.Once

// registerAII18nSeed 注册 513（真正干活的那一半）。
func registerAII18nSeed() {
	registerSeed(Seed{
		Version:   "513-ai-i18n-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.title', 'ai.err.internal', 'admin.customers.bulk.result.disabled') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 6 THEN 1 ELSE 0 END",
		SQL: mustSQL("513_ai_i18n_keys.sql"),
	})
}
