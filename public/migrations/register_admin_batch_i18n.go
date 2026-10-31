package migrations

import "sync"

func registerAdminBatchI18n() {
	registerAdminBatchI18nOnce.Do(registerAdminBatchI18nSeed)
}

var registerAdminBatchI18nOnce sync.Once

func registerAdminBatchI18nSeed() {

	registerSeed(Seed{
		Version:   "432-i18n-admin-batch",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 66 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.article.edit.visualCreate','admin.article.edit.visualGo'," +
			"'admin.article.edit.visualHeading','admin.article.edit.visualIntro'," +
			"'admin.article.list.staleHeading','admin.article.list.staleLead'," +
			"'admin.article.list.staleMoreLead','admin.article.list.staleMoreTail'," +
			"'admin.article.list.staleNone','admin.article.list.staleTail'," +
			"'admin.article.new.formHeading','admin.article.new.heading'," +
			"'admin.article.new.intro','admin.article.new.previewHeading'," +
			"'admin.article.new.previewHint','admin.article.new.slugStable'," +
			"'admin.article.new.slugStableTail','admin.article.new.submit'," +
			"'admin.article.new.submitHint','admin.article.new.titlePlaceholder'," +
			"'admin.article.new.visualLockedHint','admin.blocks.col.impact'," +
			"'admin.blocks.impact.heading','admin.blocks.impact.moreLead'," +
			"'admin.blocks.impact.moreTail','admin.blocks.impact.none'," +
			"'admin.blocks.lastError','admin.common.default'," +
			"'admin.common.media.clear'," +
			"'admin.common.media.pick','admin.coupons.status.unknown_lead'," +
			"'admin.coupons.status.unknown_tail','admin.customers.filter.hint.clickable'," +
			"'admin.depts.filter_empty','admin.mail.marketing.contact_status.pick'," +
			"'admin.mail.marketing.contact_status.target','admin.menus.filter_empty'," +
			"'admin.navigations.add_to_menu_disabled','admin.navigations.field.parent'," +
			"'admin.navigations.field.path','admin.navigations.field.target'," +
			"'admin.navigations.field.title','admin.pages.impact.count_tail'," +
			"'admin.pages.impact.heading','admin.pages.impact.more_lead'," +
			"'admin.pages.impact.more_tail','admin.pages.impact.never_published'," +
			"'admin.pages.impact.none','admin.pages.impact.scope_note'," +
			"'admin.pages.impact.unavailable','admin.pages.last_error'," +
			"'admin.pages.list.delete_confirm_prefix','admin.pages.list.delete_confirm_suffix'," +
			"'admin.products.create.title','admin.products.detailTemplate.title'," +
			"'admin.settings.locales.mode.all_prefix','admin.settings.locales.mode.default_plain'," +
			"'admin.settings.locales.mode_label','admin.settings.locales.mode.off'," +
			"" +
			"" +
			"'admin.theme.confirm_delete_note','admin.theme.empty_desc'," +
			"'admin.theme.empty_title','admin.theme.no_project_action'," +
			"'admin.theme.no_project_desc','admin.inventory.adjust.direction.adjust'," +
			"'admin.inventory.adjust.direction.out')",
		SQL: mustSQL("432_i18n_admin_batch_i18n.sql"),
	})
}

func init() { registerAdminBatchI18n() }
