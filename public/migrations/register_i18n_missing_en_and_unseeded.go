package migrations

func init() {

	registerSeed(Seed{
		Version:   "447-i18n-missing-en-and-unseeded",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(lang = 'en-US' AND item_key IN (" +
			"'MsgAdminGenericFailed','MsgBlocksTitle'," +
			"'MsgMenusTitle','MsgNavigationsTitle'," +
			"'MsgRolesTitle','MsgSiteSettingsSaved','MsgSiteSettingsTitle','MsgThemeSettingsTitle'," +
			"'MsgThemesTitle','admin.media.heading','admin.article.edit.unavailable')) OR " +
			"(lang = 'zh-CN' AND item_key IN ('admin.media.heading','admin.article.edit.unavailable'))",
		// 注：本批 SQL 的 13 行 = en-US 11 行 + zh-CN 2 行；四组页面标题的 en-US 行
		// （MsgAdministratorsTitle / MsgDatarulesTitle / MsgDepartmentsTitle /
		// MsgPermissionsTitle）已随 735 的短 key 迁移删除，不再出现在本批。
		SQL: mustSQL("447_i18n_missing_en_and_unseeded.sql"),
	})
}
