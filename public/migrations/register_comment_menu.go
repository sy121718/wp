package migrations

func init() {
	registerSeed(Seed{
		Version:   "467-comment-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE type = 2 AND deleted_at IS NULL AND path IN ('/admin/comments')",
		SQL: mustSQL("467_comment_menu.sql"),
	})
}
