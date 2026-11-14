package migrations

func init() {

	// 447 的现行执行源是 758b：历史原文 447_i18n_missing_en_and_unseeded.sql 按
	// public/CLAUDE.md「历史迁移 SQL 不改」保持原样归档，不再注册执行。
	// 差别只有四行 —— 735 已把管理员 / 权限资源 / 部门管理 / 数据权限四个页面标题
	// 迁成短 key 并删掉旧 key，原文仍会播种它们的 en-US 行，于是每次补种都把
	// 单侧 key 造回来。758b 去掉这四行，守门与执行源逐条对齐（13 行）。
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
		SQL: mustSQL("758b_current_447_i18n_missing_en_and_unseeded.sql"),
	})
}
