package migrations

// register_drop_media_empty_i18n.go — 480：删除退役的「未选择图片」词条。
//
// 注册方式：本文件自带 init()（与 479/478/477 同形）。
//
// 这是**删除类** seed，与新增类的判据方向相反：门槛要求「已经不在了」才跳过。
// 配套的 432 收口见 480 的 SQL 头注释 —— 缺那一半，这个删除每次启动都会被 seed 赢回去。
func init() {
	registerSeed(Seed{
		Version:   "480-drop-media-empty-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.common.media.empty'",
		SQL: mustSQL("480_drop_media_empty_i18n.sql"),
	})
}
