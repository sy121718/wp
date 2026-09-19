package migrations

// register_content_template_structure_i18n.go — 结构模板后台改造的词条（迁移 291）。
//
// 单独一个主题文件：这批词条服务于结构模板的后台改造（内容模板列表页的生效 / 引用列、
// 主题设置页的结构模板下拉），与 register_admin_remaining_templates_i18n.go（商品详情 /
// 邮件营销）、register_bulk_notice_i18n.go（批量结论）各是不同主题 —— 混在一个函数里会让
// 「这批词条是谁的」在 review 时看不出来。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册 —— 注册顺序在 diff 里可见是有意的）。
func registerContentTemplateStructureI18n() {
	// 291：20 个 key × 中英 = 40 行。
	//
	// 判定枚举本批全部 20 个 key（>=20），不用全表行数：那会被同期其它批次的行满足而
	// 静默跳过（本仓库踩过，理由见 226/277）。ConditionSQL 里没有占位符 —— 迁移器传进来的
	// ? 是表名，拿它当 key 会让判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "291-content-template-structure-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.content.templates.introStructure', 'admin.content.templates.colType', 'admin.content.templates.colStatus', " +
			"'admin.content.templates.colRefs', 'admin.content.templates.activeBadge', 'admin.content.templates.activate', " +
			"'admin.content.templates.refPages', 'admin.content.templates.refInstances', 'admin.content.templates.refNone', " +
			"'admin.content.templates.refUnknown', 'admin.content.templates.entityHeader', 'admin.content.templates.entityFooter', " +
			"'admin.content.templates.roleLabel', 'admin.content.templates.noVisualEdit', " +
			"'admin.theme_settings.structure_templates', 'admin.theme_settings.header_template', " +
			"'admin.theme_settings.footer_template', 'admin.theme_settings.structure_none', " +
			"'admin.theme_settings.structure_none_footer', 'admin.theme_settings.structure_hint')",
		SQL: mustSQL("291_content_template_structure_ui_i18n.sql"),
	})
}
