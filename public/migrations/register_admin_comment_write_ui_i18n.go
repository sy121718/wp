package migrations

import "sync"

func registerAdminCommentWriteUII18n() {
	registerAdminCommentWriteUII18nOnce.Do(registerAdminCommentWriteUII18nSeed)
}

var registerAdminCommentWriteUII18nOnce sync.Once

func registerAdminCommentWriteUII18nSeed() {
	// The unique key bounds this guard to two keys times two languages.
	// A third language cannot hide a missing pair; conflicts preserve operator edits.
	registerSeed(Seed{
		Version:   "607-admin-comment-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.comment.write.refresh', 'admin.comment.write.noJs')",
		SQL: mustSQL("607_admin_comment_write_ui_i18n.sql"),
	})
}
