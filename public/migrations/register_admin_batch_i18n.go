package migrations

import "sync"

// register_admin_batch_i18n.go — 后台散词条补漏（432）。
//
// 见 432_i18n_admin_batch_i18n.sql 的头部：本批是全量扫描的产物（模板三种取词形态 + Go 侧取词
// 与 sys_i18n 做差集），收录实体页面级的新功能词条 —— 文章列表与新建页的待重建影响面、
// 可视化编辑入口、块列表影响面、页面列表的全站待重建卡、主题与站点槽位空态、多语言路径方案，
// 以及 Go 侧库存调整方向的两个下拉项。批量条文案在 434、代客建单整页在 433。
//
// 注册方式：本文件自带 init()（与 register_customers_status_help_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerAdminBatchI18n() {
	registerAdminBatchI18nOnce.Do(registerAdminBatchI18nSeed)
}

// registerAdminBatchI18nOnce 让重复调用成为空操作。
var registerAdminBatchI18nOnce sync.Once

// registerAdminBatchI18nSeed 注册 432（真正干活的那一半）。
func registerAdminBatchI18nSeed() {
	// 门槛 = 本批 70 个新 key 的 **en-US** 行都在（= 71 行）。挑 en-US 而不是 zh-CN：
	// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（416 / 431 的同一理由）。
	//
	// 本批与 400 有两处 key 重叠（admin.depts.filter_empty / admin.menus.filter_empty，
	// 400 已注册但当前未落库）。重叠让门槛多出 2 行的余量，远小于 71，不会造成提前成立。
	//
	// 放在**种子**台账：本批只新增词条、不改任何既有 key，与其它 seed 无先后约束，
	// 但词条属于 seed 语义（可重复写入的默认值），与 416 / 431 保持一致。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "432-i18n-admin-batch",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 70 THEN 1 ELSE 0 END FROM sys_i18n " +
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
			"'admin.site_slots.bind_panel.page','admin.site_slots.load_failed.reload'," +
			"'admin.site_slots.no_project_list','admin.site_slots.select_page_placeholder'," +
			"'admin.theme.confirm_delete_note','admin.theme.empty_desc'," +
			"'admin.theme.empty_title','admin.theme.no_project_action'," +
			"'admin.theme.no_project_desc','admin.inventory.adjust.direction.adjust'," +
			"'admin.inventory.adjust.direction.out')",
		SQL: mustSQL("432_i18n_admin_batch_i18n.sql"),
	})
}

func init() { registerAdminBatchI18n() }
