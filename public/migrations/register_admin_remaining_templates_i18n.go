package migrations

// register_admin_remaining_templates_i18n.go — 后台模板剩余硬编码文案的词条（迁移 284）。
//
// 单独一个主题文件：这批词条服务于三个后台页面（商品详情页模板面板 / 商品新建提示 /
// 邮件营销导入占位符），与 register_bulk_notice_i18n.go（批量结论）、
// register_plugin_patrol_i18n.go（插件巡检）各是不同主题，混在一个函数里会让
// 「这批词条是谁的」在 review 时看不出来。
//
// 注册方式：由 register.go 的 init() 显式调用（不要在文件尾加 init() 自注册 ——
// 注册顺序在 diff 里可见是有意的）。
func registerAdminRemainingTemplatesI18n() {
	// 284：21 个 key × 中英 = 42 行。
	//
	// 判定枚举本批全部 21 个 key（>=21），不用全表行数：用全库行数会被同期其它批次的行
	// 满足而静默跳过（本仓库踩过，理由见 226/277）。ConditionSQL 里没有占位符 ——
	// 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "284-admin-remaining-templates-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 21 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.product_detail.template.mode.document', 'admin.product_detail.template.mode.template', 'admin.product_detail.template.current', 'admin.product_detail.template.customize', 'admin.product_detail.template.edit', 'admin.product_detail.template.affectedLead', 'admin.product_detail.template.affectedTail', 'admin.product_detail.template.docHint', 'admin.product_detail.template.presetUpdated', 'admin.product_detail.template.reapplyPreset', 'admin.product_detail.template.rollbackLabel', 'admin.product_detail.template.rollbackSubmit', 'admin.product_detail.template.rollbackHint', 'admin.product_detail.template.presetModeHint', 'admin.product_detail.template.unpublishedHint', 'admin.product_detail.template.noPublicationHint', 'admin.products.new.template.boundHint', 'admin.products.new.template.noneHint', 'admin.products.new.template.unavailableHint', 'admin.mail.marketing.import.tags.ph', 'admin.mail.marketing.import.content.ph')",
		SQL: mustSQL("284_admin_remaining_templates_i18n.sql"),
	})
}
