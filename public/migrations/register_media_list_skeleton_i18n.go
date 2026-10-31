package migrations

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
