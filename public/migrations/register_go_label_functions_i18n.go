package migrations

// 457 — Go 侧「返回中文的展示标签函数」收口到词条（contenttemplate / 集合源 / mail）。
//
// 门槛判据**枚举本批自己的 key**（上界封闭，18 个 key × 2 语言 = 36 行）：
// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的故障），
// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
// 判据的偏差方向刻意选「宁可重跑，不可静默跳过」——所以这里一旦漏写一个 key，
// 表现是每次启动都重跑这条 seed（幂等 SQL，代价可接受），而不是词条悄悄没进去。
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
