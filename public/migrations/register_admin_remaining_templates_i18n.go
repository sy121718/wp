package migrations

func registerAdminRemainingTemplatesI18n() {

	registerSeed(Seed{
		Version:   "284-admin-remaining-templates-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 21 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.product_detail.template.mode.document', 'admin.product_detail.template.mode.template', 'admin.product_detail.template.current', 'admin.product_detail.template.customize', 'admin.product_detail.template.edit', 'admin.product_detail.template.affectedLead', 'admin.product_detail.template.affectedTail', 'admin.product_detail.template.docHint', 'admin.product_detail.template.presetUpdated', 'admin.product_detail.template.reapplyPreset', 'admin.product_detail.template.rollbackLabel', 'admin.product_detail.template.rollbackSubmit', 'admin.product_detail.template.rollbackHint', 'admin.product_detail.template.presetModeHint', 'admin.product_detail.template.unpublishedHint', 'admin.product_detail.template.noPublicationHint', 'admin.products.new.template.boundHint', 'admin.products.new.template.noneHint', 'admin.products.new.template.unavailableHint', 'admin.mail.marketing.import.tags.ph', 'admin.mail.marketing.import.content.ph')",
		SQL: mustSQL("284_admin_remaining_templates_i18n.sql"),
	})
}
