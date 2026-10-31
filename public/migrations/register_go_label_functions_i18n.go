package migrations

func init() {
	registerSeed(Seed{
		Version:   "457-i18n-go-label-functions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 36 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.content.templates.entityProduct', " +
			"'admin.content.templates.entityArticle', 'admin.content.templates.entityCategory', " +
			"'admin.content.templates.entityTag', 'admin.content.templates.entityBrand', " +
			"'admin.content.templates.roleArchive', 'admin.content.templates.roleDetail', " +
			"'admin.content.templates.slotHeader', 'admin.content.templates.slotFooter', " +
			"'admin.collection.article', 'admin.collection.product', 'admin.collection.category', " +
			"'admin.mail.automation.status.unknown', 'admin.mail.automation.statusChanged', " +
			"'admin.mail.test_send.temporary', 'admin.mail.test_send.permanent', " +
			"'admin.mail.test_send.configuration', 'admin.mail.test_send.unknown') " +
			"AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("457_i18n_go_label_functions.sql"),
	})
}
