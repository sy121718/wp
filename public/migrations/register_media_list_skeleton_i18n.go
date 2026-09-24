package migrations

// register_media_list_skeleton_i18n.go — 443：媒体列表页骨架新增词条（中英成对）。
//
// ConditionSQL 判定「9 个 key 的 zh-CN + en-US 共 18 行是否齐全」：齐全即跳过；
// 缺任一行就重放 SQL 补齐（INSERT ... ON CONFLICT DO NOTHING 不覆盖运营改过的值）。
func init() {
	registerSeed(Seed{
		Version:   "443-i18n-media-list-skeleton",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.media.filter.label','admin.media.list.title','admin.media.list.aria'," +
			"'admin.media.bulk.selected','admin.media.bulk.download_scope','admin.media.bulk.delete'," +
			"'admin.media.bulk.select_all','admin.media.bulk.select_all_aria','admin.media.col.actions') " +
			"AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("443_i18n_media_list_skeleton.sql"),
	})
}
