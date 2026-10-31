package migrations

func init() {
	registerSeed(Seed{
		Version:   "459-i18n-page-mail-err-detail",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 44 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +

			"'admin.page.detail.documentEmpty', 'admin.page.detail.nodeDepthExceed', " +
			"'admin.page.detail.nodeInvalid', 'admin.page.detail.settingsInvalid', " +
			"'admin.page.detail.structureInvalid', " +

			"'admin.mail.detail.graphEmpty', 'admin.mail.detail.requestNotJSON', " +
			"'admin.mail.detail.notGraph', 'admin.mail.detail.noNode', " +
			"'admin.mail.detail.nodeKeyMissing', 'admin.mail.detail.nodeKeyDuplicate', " +
			"'admin.mail.detail.needMinutes', 'admin.mail.detail.needTemplate', " +
			"'admin.mail.detail.needTwoArms', 'admin.mail.detail.needCondition', " +
			"'admin.mail.detail.needTagAction', 'admin.mail.detail.unknownNodeType', " +
			"'admin.mail.detail.entryMissing', 'admin.mail.detail.entryNotExist', " +
			"'admin.mail.detail.edgeTargetMissing', 'admin.mail.detail.cycle', " +
			"'admin.mail.detail.unreachable'" +
			") AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("459_i18n_page_mail_err_detail.sql"),
	})
}
