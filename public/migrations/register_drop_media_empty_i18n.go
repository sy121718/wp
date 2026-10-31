package migrations

func init() {
	registerSeed(Seed{
		Version:   "480-drop-media-empty-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.common.media.empty'",
		SQL: mustSQL("480_drop_media_empty_i18n.sql"),
	})
}
