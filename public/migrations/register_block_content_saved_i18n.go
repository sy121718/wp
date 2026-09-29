package migrations

// register_block_content_saved_i18n.go — 迁移 453 的 seed 注册。
//
// 门槛：这一条 key 的中英两行都在库时才跳过（ConditionSQL 返回 > 0 即跳过）；
// 少一行就重跑 SQL —— SQL 自身 ON CONFLICT DO NOTHING，重跑不会覆盖已人工改过的译文。
func init() {
	registerSeed(Seed{
		Version:   "453-i18n-block-content-saved",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.blocks.content.savedRebuild' AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("453_i18n_block_content_saved.sql"),
	})
}
