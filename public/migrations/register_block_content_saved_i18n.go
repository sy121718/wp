package migrations

func init() {
	registerSeed(Seed{
		Version:   "453-i18n-block-content-saved",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.blocks.content.savedRebuild' AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("453_i18n_block_content_saved.sql"),
	})
}
