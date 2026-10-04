package migrations

import "sync"

// register_ai_token_i18n.go — 对外访问令牌的词条（543）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：挑两个**代表 key**（各自的中英两条都必须在），
// 本批共 6 个 key × 2 语言 = 12 条元组，用「代表 key 的语言齐全」当门槛。
// INSERT 侧 ON CONFLICT DO NOTHING，重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAITokenI18n() {
	registerAITokenI18nOnce.Do(registerAITokenI18nSeed)
}

// registerAITokenI18nOnce 让重复调用成为空操作。
var registerAITokenI18nOnce sync.Once

// registerAITokenI18nSeed 注册 543（真正干活的那一半）。
func registerAITokenI18nSeed() {
	registerSeed(Seed{
		Version:   "543-ai-token-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('ai.err.tokenInvalid', 'ai.err.tokenNameRequired') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("543_ai_token_i18n.sql"),
	})
}
