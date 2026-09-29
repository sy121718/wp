package migrations

// register_mail_masterdata_webhook_text_i18n.go — 迁移 450 的 seed 注册。
//
// 门槛：84 个 key × 2 语言 = 168 行**全部**在库时才跳过（ConditionSQL 返回 > 0 即跳过）；
// 少一行就重跑 SQL —— SQL 自身 ON CONFLICT DO NOTHING，重跑不会覆盖已人工改过的译文。
func init() {
	registerSeed(Seed{
		Version:   "450-i18n-mail-masterdata-webhook-text",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 168 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.mail.automation.trigger.contact_created', 'admin.mail.automation.trigger.contact_subscribed', 'admin.mail.automation.trigger.email_clicked', 'admin.mail.automation.trigger.email_opened', " +
			"'admin.mail.automation.trigger.manual', 'admin.mail.automation.trigger.tag_added', 'admin.mail.automation_canvas.title', 'admin.mail.automation_edit.node.branch.hint', " +
			"'admin.mail.automation_edit.node.branch.label', 'admin.mail.automation_edit.node.delay.hint', 'admin.mail.automation_edit.node.delay.label', 'admin.mail.automation_edit.node.email.hint', " +
			"'admin.mail.automation_edit.node.email.label', 'admin.mail.automation_edit.node.end.hint', 'admin.mail.automation_edit.node.end.label', 'admin.mail.automation_edit.node.tag.hint', " +
			"'admin.mail.automation_edit.node.tag.label', 'admin.mail.automation_edit.node.trigger.hint', 'admin.mail.automation_edit.node.trigger.label', 'admin.mail.automation_edit.row.none', " +
			"'admin.mail.automation_edit.title', 'admin.mail.campaign.heading', 'admin.masterdata.action.create', 'admin.masterdata.action.delete', " +
			"'admin.masterdata.action.update', 'admin.masterdata.entity.inventory_source', 'admin.masterdata.entity.product', 'admin.masterdata.entity.product_variant', " +
			"'admin.masterdata.field.inventory_source.code', 'admin.masterdata.field.inventory_source.config', 'admin.masterdata.field.inventory_source.name', 'admin.masterdata.field.inventory_source.related_party', " +
			"'admin.masterdata.field.inventory_source.settle_price', 'admin.masterdata.field.inventory_source.sort', 'admin.masterdata.field.inventory_source.status', 'admin.masterdata.field.inventory_source.type', " +
			"'admin.masterdata.field.product.brand_id', 'admin.masterdata.field.product.default_price', 'admin.masterdata.field.product.name', 'admin.masterdata.field.product.slug', " +
			"'admin.masterdata.field.product.status', 'admin.masterdata.field.product_variant.barcode', 'admin.masterdata.field.product_variant.compare_price', 'admin.masterdata.field.product_variant.cost_price', " +
			"'admin.masterdata.field.product_variant.enabled', 'admin.masterdata.field.product_variant.home_warehouse_code', 'admin.masterdata.field.product_variant.home_warehouse_id', 'admin.masterdata.field.product_variant.option_values', " +
			"'admin.masterdata.field.product_variant.price', 'admin.masterdata.field.product_variant.sku_code', 'mail.run.automationDeleted', 'mail.run.branchNo', " +
			"'mail.run.branchYes', 'mail.run.condMissing', 'mail.run.condTagMissing', 'mail.run.condUnknown', " +
			"'mail.run.contactMissing', 'mail.run.definitionInvalid', 'mail.run.delayContinue', 'mail.run.emailSent', " +
			"'mail.run.emailSentSubject', 'mail.run.emailSuppressed', 'mail.run.emailSuppressedSubject', 'mail.run.ended', " +
			"'mail.run.explain.completed', 'mail.run.explain.entryNode', 'mail.run.explain.failed', 'mail.run.explain.reasonUnknown', " +
			"'mail.run.explain.running', 'mail.run.explain.stopped', 'mail.run.explain.stoppedReason', 'mail.run.explain.unknownStatus', " +
			"'mail.run.explain.waiting', 'mail.run.explain.whenUnset', 'mail.run.nodeMissing', 'mail.run.sendFailed', " +
			"'mail.run.stepsExceeded', 'mail.run.tagsApplied', 'mail.run.triggerEntered', 'mail.run.unknownNodeType', " +
			"'mail.test.mailHtml', 'webhook.delivery.err.cipherUnavailable', 'webhook.delivery.err.endpointMissing', 'webhook.delivery.err.remoteStatus') " +
			"AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("450_i18n_mail_masterdata_webhook_text.sql"),
	})
}
