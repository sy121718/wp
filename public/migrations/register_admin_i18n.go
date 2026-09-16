package migrations

// registerAdminI18nSeedsAndLatest 注册「后台 i18n 词条与后续迁移（176–197）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerAdminI18nSeedsAndLatest() {
	// 176：商品列表组件固定文案的中英词条（审计 I18N-010）。
	// 门槛按 productList 自己的 zh-CN 行数判定，而不是模糊匹配全部 site.component.*：
	// 后者会被别的组件 seed 顺带满足，导致本迁移「看起来已应用、词条却没写进去」。
	registerSeed(Seed{
		Version:      "176-i18n-seed-product-list",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 16 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'site.component.productList.%'",
		SQL:          mustSQL("176_i18n_seed_product_list.sql"),
	})

	// 177：其余组件固定文案的补全词条（审计 I18N-010）。
	// 判定按**本批自己的 key 全集合**（而不是 site.component.% 总数）：
	// 总数门槛会被别的批次顺带满足 —— 060 就是这么被跳过的（见那里的注释）。
	registerSeed(Seed{
		Version:   "177-i18n-seed-component-texts",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 26 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'site.component.product.stockNote', 'site.component.searchResults.label', " +
			"'site.component.searchResults.submit', 'site.component.orderList.loginHint', " +
			"'site.component.orderList.loginText', 'site.component.orderList.pagesText', " +
			"'site.component.orderList.notice', 'site.component.userForms.scriptHint', " +
			"'site.component.userForms.openFallback', 'site.component.cardStack.dragAria', " +
			"'site.component.cardStack.deckAria', 'site.component.cardStack.zoomAria', " +
			"'site.component.cardStack.zoomCardAria', 'site.component.cardStack.prev', " +
			"'site.component.cardStack.next', 'site.component.cardStack.closeZoomAria', " +
			"'site.component.cardStack.close', 'site.component.userForms.title.login', " +
			"'site.component.userForms.title.register', 'site.component.userForms.title.forgot', " +
			"'site.component.userForms.title.reset', 'site.component.userForms.title.account', " +
			"'site.component.userForms.title.profile', 'site.component.userForms.title.preference', " +
			"'site.component.userForms.title.password', 'site.component.userForms.title.sessions')",
		SQL: mustSQL("177_i18n_seed_component_texts.sql"),
	})

	// 178：文案词条管理的权限点与后台菜单（审计 I18N-003）。
	// 一个写权限点覆盖保存与删除：两者是同一件事的两种动作，
	// 拆开只会让「给了保存、忘了删除」有机会发生。
	//
	// 判定必须把权限点代码写进 SQL 字面量：CheckSQL 里的 ? 由迁移器传的是**表名**
	// （sys_permission），写成 permission_code = ? 等于永远查不到行 —— 这条迁移会每次启动
	// 都重跑。以前被 SQL 里 WHERE NOT EXISTS 的幂等性掩盖，直到 208 把 sys_menus.deleted_time
	// 改名（本迁移 SQL 引用旧列名）才暴露成启动失败。
	// ? 仍保留在「表存在」这一项上（护栏要求自定义判定接收表名参数），语义也更严：
	// 表都没了就不该算完成，启动时应当停下来而不是静默跳过。
	register(Migration{
		Version:   "178-i18n-manage-permission",
		TableName: "sys_permission",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND (SELECT COUNT(*) FROM sys_permission " +
			"WHERE permission_code = 'i18n:manage') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("178_i18n_manage_permission.sql"),
	})

	// 179：购物车模块文案词条（审计 I18N-002）。
	// 判据按**本批自己的 key** 计数（而不是按 site.component.% 这类总量）—— 用总量会被
	// 后来批次的行满足，于是本批在干净库上被静默跳过（060 踩过这个坑）。
	registerSeed(Seed{
		Version:      "179-i18n-seed-cart",
		TableName:    "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 22 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('cart.msg.updated', 'cart.msg.cleared', 'cart.err.invalidParam', 'cart.err.projectRequired', 'cart.err.variantRequired', 'cart.err.quantityInvalid', 'cart.err.quantityTooMany', 'cart.err.cartEmpty', 'cart.err.cartFull', 'cart.err.cartItemAbsent', 'cart.err.variantNotFound', 'cart.err.outOfStock', 'cart.err.emailRequired', 'cart.err.emailInvalid', 'cart.err.nameRequired', 'cart.err.phoneRequired', 'cart.err.addressRequired', 'cart.err.paymentFailed', 'cart.err.callbackSignature', 'cart.err.callbackOrderMissing', 'cart.err.callbackAmountMismatch', 'cart.err.internal')`,
		SQL:          mustSQL("179_i18n_seed_cart.sql"),
	})

	// 180：order 模块文案词条（审计 I18N-002）。
	// 判据按**本批自己的 key** 计数 —— 用总量会被后来批次的行满足，本批就被静默跳过了。
	registerSeed(Seed{
		Version:      "180-i18n-seed-order",
		TableName:    "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 62 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('order.msg.createSuccess', 'order.msg.statusChanged', 'order.msg.cancelled', 'order.msg.cancelledStockWarning', 'order.msg.refunded', 'order.msg.paid', 'order.msg.noteUpdated', 'order.err.invalidParam', 'order.err.projectRequired', 'order.err.itemsRequired', 'order.err.itemLimitExceeded', 'order.err.quantityInvalid', 'order.err.customerEmailRequired', 'order.err.customerEmailInvalid', 'order.err.orderNoInvalid', 'order.err.statusInvalid', 'order.err.noteTooLong', 'order.err.orderNotFound', 'order.err.orderNoTaken', 'order.err.orderHasNoItems', 'order.err.statusTransition', 'order.err.orderNotCancellable', 'order.err.orderNotRefundable', 'order.err.alreadyCancelled', 'order.err.alreadyRefunded', 'order.err.cancelReasonRequired', 'order.err.paymentMethodRequired', 'order.err.paymentChannelFailed', 'order.msg.returnRequested', 'order.msg.returnApproved', 'order.msg.returnRejected', 'order.msg.returnReceived', 'order.msg.returnCancelled', 'order.err.returnNotFound', 'order.err.returnItemsRequired', 'order.err.returnQuantityInvalid', 'order.err.returnQuantityExceeded', 'order.err.returnReasonRequired', 'order.err.returnNotCancellable', 'order.err.returnOrderNotReturnable', 'order.err.returnNotReviewable', 'order.err.returnNotReceivable', 'order.err.returnRejectReasonRequired', 'order.err.variantNotFound', 'order.err.stockInsufficient', 'order.err.stockUnavailable', 'order.msg.couponCreated', 'order.msg.couponUpdated', 'order.msg.couponDeleted', 'order.err.couponNotFound', 'order.err.couponCodeRequired', 'order.err.couponCodeTaken', 'order.err.couponTypeInvalid', 'order.err.couponValueInvalid', 'order.err.couponWindowInvalid', 'order.err.couponDisabled', 'order.err.couponNotStarted', 'order.err.couponExpired', 'order.err.couponExhausted', 'order.err.couponUserLimit', 'order.err.couponMinSubtotal', 'order.err.couponInUse')`,
		SQL:          mustSQL("180_i18n_seed_order.sql"),
	})

	// 181：user 模块文案词条（审计 I18N-002）。
	// 判据按**本批自己的 key** 计数 —— 用总量会被后来批次的行满足，本批就被静默跳过了。
	registerSeed(Seed{
		Version:      "181-i18n-seed-user",
		TableName:    "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 41 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('user.msg.registerSuccess', 'user.msg.activateSuccess', 'user.msg.loginSuccess', 'user.msg.logoutSuccess', 'user.msg.profileSaved', 'user.msg.preferenceSaved', 'user.msg.passwordChanged', 'user.msg.resetMailSent', 'user.err.invalidParam', 'user.err.usernameRequired', 'user.err.usernameTooShort', 'user.err.usernameTooLong', 'user.err.usernameTaken', 'user.err.emailRequired', 'user.err.emailInvalid', 'user.err.emailTaken', 'user.err.passwordTooShort', 'user.err.userNotFound', 'user.err.activationInvalid', 'user.err.mailUnavailable', 'user.err.loginRequired', 'user.err.badCredentials', 'user.err.accountLocked', 'user.err.accountDisabled', 'user.err.accountPending', 'user.err.passwordLoginUnavailable', 'user.err.sessionNotFound', 'user.err.sessionExpired', 'user.err.logoutFailed', 'user.err.oldPasswordWrong', 'user.err.newPasswordSame', 'user.err.profileVisibilityInvalid', 'user.err.pageSizeInvalid', 'user.err.notLoggedIn', 'user.err.internal', 'user.msg.customerEnabled', 'user.msg.customerDisabled', 'user.msg.customerUnlocked', 'user.msg.customerNotLocked', 'user.msg.customerFailuresCleared', 'user.err.customerStatusInvalid')`,
		SQL:          mustSQL("181_i18n_seed_user.sql"),
	})

	// 182：mail 模块文案词条（审计 I18N-002）。
	// 判据按**本批自己的 key** 计数 —— 用总量会被后来批次的行满足，本批就被静默跳过了。
	registerSeed(Seed{
		Version:      "182-i18n-seed-mail",
		TableName:    "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 32 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('mail.msg.sendSuccess', 'mail.msg.saveSuccess', 'mail.msg.deleteSuccess', 'mail.msg.importSuccess', 'mail.msg.testSent', 'mail.msg.campaignStarted', 'mail.msg.automationStarted', 'mail.err.invalidParam', 'mail.err.accountNotFound', 'mail.err.templateNotFound', 'mail.err.contactNotFound', 'mail.err.campaignNotFound', 'mail.err.accountDisabled', 'mail.err.accountIncomplete', 'mail.err.suppressed', 'mail.err.emailRequired', 'mail.err.emailInvalid', 'mail.err.importEmpty', 'mail.err.importTooLarge', 'mail.err.campaignNotDraft', 'mail.err.campaignNoRecipient', 'mail.err.cipherSecretMissing', 'mail.err.cipherUnavailable', 'mail.err.templateSyntax', 'mail.err.campaignSending', 'mail.err.automationNotFound', 'mail.err.automationTriggerInvalid', 'mail.err.automationGraphInvalid', 'mail.err.automationRunExists', 'mail.err.automationRunNotFound', 'mail.test.mailSubject', 'mail.test.mailText')`,
		SQL:          mustSQL("182_i18n_seed_mail.sql"),
	})

	// 183：产物 SEO 体检权限点（审计 SEO-019）。
	register(Migration{
		Version:   "183-seo-audit-permission",
		TableName: "sys_permission",
		CheckSQL:  "SELECT COUNT(*) FROM sys_permission WHERE permission_code = ?",
		SQL:       mustSQL("183_seo_audit_permission.sql"),
	})

	// 187：后台模板文案词条（审计 I18N-001 第一批：settings / plugins / media 三页）。
	// 判据按**本批自己的 key** 计数 —— 用总量会被后来批次的行满足，本批就被静默跳过了。
	registerSeed(Seed{
		Version:   "187-i18n-seed-admin-pages",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 101 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.media.action.batch_download', 'admin.media.action.batch_download_title', 'admin.media.action.close', 'admin.media.action.save', " +
			"'admin.media.action.upload', 'admin.media.category.name', 'admin.media.category.parent', 'admin.media.col.category', " +
			"'admin.media.col.file', 'admin.media.col.size', 'admin.media.col.type', 'admin.media.col.uploaded_at', " +
			"'admin.media.detail.close', 'admin.media.detail.title', 'admin.media.empty', 'admin.media.pager.label', " +
			"'admin.media.pager.next', 'admin.media.pager.prev', 'admin.media.search_placeholder', 'admin.media.tree.all', " +
			"'admin.media.tree.new', 'admin.media.tree.new_category', 'admin.media.tree.search_placeholder', 'admin.media.tree.title', " +
			"'admin.media.upload.category_label', 'admin.media.upload.drop_hint', 'admin.media.view.grid', 'admin.media.view.list', " +
			"'admin.plugins.action.uninstall', 'admin.plugins.col.actions', 'admin.plugins.col.component_count', 'admin.plugins.col.installed_at', " +
			"'admin.plugins.col.name', 'admin.plugins.col.status', 'admin.plugins.col.version', 'admin.plugins.confirm.uninstall', " +
			"'admin.plugins.install.hint', 'admin.plugins.install.submit', 'admin.plugins.install.title', 'admin.plugins.list.empty', " +
			"'admin.plugins.list.title', 'admin.plugins.status.disabled', 'admin.plugins.status.enabled', 'admin.settings.action.save', " +
			"'admin.settings.col.default', 'admin.settings.col.kind', 'admin.settings.col.pattern', 'admin.settings.empty', " +
			"'admin.settings.field.contact_email', 'admin.settings.field.ga4_measurement_id', 'admin.settings.field.gsc_token', 'admin.settings.field.indexnow_key', " +
			"'admin.settings.field.name', 'admin.settings.field.site_desc', 'admin.settings.field.site_name', 'admin.settings.field.url_structure', " +
			"'admin.settings.hint.ga4.after_code', 'admin.settings.hint.ga4.lead', 'admin.settings.hint.ga4.republish', 'admin.settings.hint.ga4.tail', " +
			"'admin.settings.hint.gsc.after', 'admin.settings.hint.gsc.lead', 'admin.settings.hint.gsc.republish', 'admin.settings.hint.gsc.tail', " +
			"'admin.settings.hint.indexnow.after_code', 'admin.settings.hint.indexnow.lead', 'admin.settings.hint.indexnow.verify_path', 'admin.settings.hint.url.after_id', " +
			"'admin.settings.hint.url.after_slash', 'admin.settings.hint.url.after_slug', 'admin.settings.hint.url.lead', 'admin.settings.hint.url.strong', " +
			"'admin.settings.hint.url.tail', 'admin.settings.hint.url_stability.lead', 'admin.settings.hint.url_stability.strong', 'admin.settings.hint.url_stability.tail', " +
			"'admin.settings.locales.action.add', 'admin.settings.locales.action.save', 'admin.settings.locales.hint.after_modes', 'admin.settings.locales.hint.mode_all_prefix', " +
			"'admin.settings.locales.hint.mode_body', 'admin.settings.locales.hint.mode_default_plain', 'admin.settings.locales.hint.mode_intro', 'admin.settings.locales.hint.mode_off', " +
			"'admin.settings.locales.hint.note_lead', 'admin.settings.locales.hint.note_strong', 'admin.settings.locales.hint.note_tail', 'admin.settings.locales.hint.rules', " +
			"'admin.settings.locales.ph.new_lang', 'admin.settings.locales.saved', 'admin.settings.locales.title', 'admin.settings.ph.gsc_token', " +
			"'admin.settings.ph.indexnow_key', 'admin.settings.ph.site_desc', 'admin.settings.ph.site_name', 'admin.settings.roadmap.hint', " +
			"'admin.settings.roadmap.item.footer', 'admin.settings.roadmap.item.security', 'admin.settings.roadmap.item.seo', 'admin.settings.roadmap.title', " +
			"'admin.settings.title')",
		SQL: mustSQL("187_i18n_seed_admin_pages.sql"),
	})

	// 188：后台「文章 / 内容模板」页面文案词条（审计 I18N-001 第二批：article* / content* 模板）。
	// 判据按**本批自己的 key** 计数 —— 用总量会被后来批次的行满足，本批就被静默跳过了。
	registerSeed(Seed{
		Version:   "188-i18n-seed-admin-article",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 149 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.article.list.heading', 'admin.article.list.lastError', 'admin.article.list.intro', " +
			"'admin.article.list.introTail', 'admin.article.list.slugStable', 'admin.article.list.slugStableTail', " +
			"'admin.article.list.createHeading', 'admin.article.list.titlePlaceholder', 'admin.article.list.slugPlaceholder', " +
			"'admin.article.list.createSubmit', 'admin.article.list.createHint', 'admin.article.list.allHeading', " +
			"'admin.article.list.allHeadingClose', 'admin.article.list.empty', 'admin.article.list.tableAria', " +
			"'admin.article.list.colTitle', 'admin.article.list.colSlug', 'admin.article.list.colExcerpt', " +
			"'admin.article.list.colState', 'admin.article.list.colRevision', 'admin.article.list.colUpdatedAt', " +
			"'admin.article.list.colActions', 'admin.article.list.edit', 'admin.article.list.online', " +
			"'admin.article.list.deleteConfirm', 'admin.article.list.delete', 'admin.article.list.footHint', " +
			"'admin.article.edit.backToList', 'admin.article.edit.viewOnline', 'admin.article.edit.editTemplate', " +
			"'admin.article.edit.lastError', 'admin.article.edit.newHint', 'admin.article.edit.versionPrefix', " +
			"'admin.article.edit.versionMiddle', 'admin.article.edit.rebuildLead', 'admin.article.edit.rebuildMark', " +
			"'admin.article.edit.rebuildTail', 'admin.article.edit.bodyHeading', 'admin.article.edit.titleLabel', " +
			"'admin.article.edit.titlePlaceholder', 'admin.article.edit.slugLabel', 'admin.article.edit.slugNewHint', " +
			"'admin.article.edit.slugImmutable', 'admin.article.edit.slugNewHintTail', 'admin.article.edit.slugReadonlyHint', " +
			"'admin.article.edit.excerptLabel', 'admin.article.edit.excerptPlaceholder', 'admin.article.edit.contentLabel', " +
			"'admin.article.edit.bodyHint', 'admin.article.edit.coverLabel', 'admin.article.edit.coverPlaceholder', " +
			"'admin.article.edit.coverHint', 'admin.article.edit.coverHintTail', 'admin.article.edit.seoHeading', " +
			"'admin.article.edit.focusKeywordLabel', 'admin.article.edit.focusKeywordPlaceholder', 'admin.article.edit.focusKeywordHint', " +
			"'admin.article.edit.focusKeywordHintStrong', 'admin.article.edit.focusKeywordHintTail', 'admin.article.edit.seoTitleLabel', " +
			"'admin.article.edit.seoTitlePlaceholder', 'admin.article.edit.seoTitleHint', 'admin.article.edit.seoDescLabel', " +
			"'admin.article.edit.seoDescPlaceholder', 'admin.article.edit.save', 'admin.article.edit.rescore', " +
			"'admin.article.edit.rescoreHint', 'admin.article.edit.rescoreHintStrong', 'admin.article.edit.rescoreHintTail', " +
			"'admin.article.edit.seoScoreHeading', 'admin.article.edit.seoScoreLead', 'admin.article.edit.seoScoreBasic', " +
			"'admin.article.edit.seoScoreBasicTail', 'admin.article.edit.seoScoreContent', 'admin.article.edit.seoScoreContentTail', " +
			"'admin.article.edit.seoScoreKeywords', 'admin.article.edit.seoScoreKeywordsTail', 'admin.article.edit.seoScoreLinks', " +
			"'admin.article.edit.seoScoreLinksTail', 'admin.article.edit.canonicalStrong', 'admin.article.edit.canonicalTail', " +
			"'admin.article.edit.publishHeading', 'admin.article.edit.publicPathLabel', 'admin.article.edit.stale', " +
			"'admin.article.edit.staleHint', 'admin.article.edit.upToDate', 'admin.article.edit.republish', " +
			"'admin.article.edit.changePathLabel', 'admin.article.edit.keepRedirect', 'admin.article.edit.changePathSubmit', " +
			"'admin.article.edit.changePathHint', 'admin.article.edit.publishIntro', 'admin.article.edit.projectLabel', " +
			"'admin.article.edit.urlPathLabel', 'admin.article.edit.templateLabel', 'admin.article.edit.publishSubmit', " +
			"'admin.article.edit.importHeading', 'admin.article.edit.importIntro', 'admin.article.edit.importIntroStrong', " +
			"'admin.article.edit.importIntroTail', 'admin.article.edit.importCopyStrong', 'admin.article.edit.importCopyTail', " +
			"'admin.article.edit.importPreviewLead', 'admin.article.edit.importPreviewStrong', 'admin.article.edit.importPreviewTail', " +
			"'admin.article.edit.importWhitelistLead', 'admin.article.edit.importWhitelistMid', 'admin.article.edit.importWhitelistStrong', " +
			"'admin.article.edit.importWhitelistTail', 'admin.article.edit.importEmptyBody', 'admin.article.edit.importProjectLabel', " +
			"'admin.article.edit.importPathLabel', 'admin.article.edit.importPreview', 'admin.article.edit.importCreate', " +
			"'admin.article.edit.importPreviewEmpty', 'admin.article.translations.heading', 'admin.article.translations.addressHint', " +
			"'admin.article.translations.richHint', 'admin.article.translations.summaryLead', 'admin.article.translations.summaryArticles', " +
			"'admin.article.translations.summaryFields', 'admin.article.translations.summaryTail', 'admin.article.translations.switchLang', " +
			"'admin.article.translations.hasTarget', 'admin.article.translations.sourceLabel', 'admin.article.translations.saveSubmit', " +
			"'admin.content.templates.heading', 'admin.content.templates.errorLead', 'admin.content.templates.notReady', " +
			"'admin.content.templates.intro', 'admin.content.templates.projectLabel', 'admin.content.templates.entityTypeLabel', " +
			"'admin.content.templates.entityAll', 'admin.content.templates.listHeading', 'admin.content.templates.listHeadingClose', " +
			"'admin.content.templates.empty', 'admin.content.templates.tableAria', 'admin.content.templates.colName', " +
			"'admin.content.templates.colEntityType', 'admin.content.templates.colRevision', 'admin.content.templates.colUpdatedAt', " +
			"'admin.content.templates.colActions', 'admin.content.templates.visualEdit', 'admin.content.templates.editEntry', " +
			"'admin.content.templates.footerLead', 'admin.content.templates.footerProductsLink', 'admin.content.templates.footerMiddle', " +
			"'admin.content.templates.footerArticlesLink', 'admin.content.templates.footerTail')",
		SQL: mustSQL("188_i18n_seed_admin_article.sql"),
	})

	// 189：自定义 404 页的后台文案（审计 SEO-013 的配套词条）。
	// 判据按**本批自己的 key** 枚举计数 —— 用总量会被其它批次满足而静默跳过。
	registerSeed(Seed{
		Version:      "189-i18n-seed-not-found",
		TableName:    "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 9 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.settings.field.not_found_html', 'admin.settings.ph.not_found_html', 'admin.settings.hint.not_found_html.lead', 'admin.settings.hint.not_found_html.strong', 'admin.settings.hint.not_found_html.after', 'admin.settings.hint.not_found_html.tail', 'admin.settings.hint.not_found_html.republish_lead', 'admin.settings.hint.not_found_html.republish', 'admin.settings.hint.not_found_html.republish_tail')`,
		SQL:          mustSQL("189_i18n_seed_not_found.sql"),
	})

	// 194：审计 P7 索引类四条（IDX-008 / IDX-017 / IDX-018 / IDX-020）的落地。
	// 判据按**本批自己的索引名**枚举计数 —— 用总量会被其它迁移的索引满足而静默跳过。
	// 两个索引分属 inventory_stocks 与 page_routes 两张表，故表名条件写 IN (?, page_routes)。
	register(Migration{
		Version:   "194-p7-index-audit",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename IN (?, 'page_routes') " +
			"AND indexname IN ('idx_inventory_stocks_warehouse_nonzero', 'idx_page_routes_project_kind_path')",
		SQL: mustSQL("194_p7_index_audit.sql"),
	})

	// 190：营销订单类后台模板文案（审计 I18N-001 组C）
	// 判据按本批自己的 key 枚举计数（765 个全列）—— 用总量会被其它批次满足而静默跳过。
	registerSeed(Seed{
		Version:   "190-i18n-seed-marketing",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 765 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.coupons.action.collapse', 'admin.coupons.action.delete', 'admin.coupons.action.edit', 'admin.coupons.col.actions', " +
			"'admin.coupons.col.code', 'admin.coupons.col.discount', 'admin.coupons.col.min_subtotal', 'admin.coupons.col.name', " +
			"'admin.coupons.col.per_user', 'admin.coupons.col.remark', 'admin.coupons.col.status', 'admin.coupons.col.usage', " +
			"'admin.coupons.col.window', 'admin.coupons.create.heading', 'admin.coupons.create.hint.lead', 'admin.coupons.create.hint.mid', " +
			"'admin.coupons.create.hint.mid2', 'admin.coupons.create.hint.strong_immutable', 'admin.coupons.create.hint.strong_upper', 'admin.coupons.create.hint.tail', " +
			"'admin.coupons.create.ph.code', 'admin.coupons.create.ph.discount_value', 'admin.coupons.create.ph.min_subtotal', 'admin.coupons.create.ph.name', " +
			"'admin.coupons.create.submit', 'admin.coupons.create.time_hint.lead', 'admin.coupons.create.time_hint.mid', 'admin.coupons.create.time_hint.mid2', " +
			"'admin.coupons.create.time_hint.strong', 'admin.coupons.create.time_hint.tail', 'admin.coupons.create.unit_hint.lead', 'admin.coupons.create.unit_hint.mid', " +
			"'admin.coupons.create.unit_hint.mid2', 'admin.coupons.create.unit_hint.mid3', 'admin.coupons.create.unit_hint.mid4', 'admin.coupons.create.unit_hint.mid5', " +
			"'admin.coupons.create.unit_hint.strong', 'admin.coupons.create.unit_hint.strong_strength', 'admin.coupons.create.unit_hint.tail', 'admin.coupons.detail.code_aria', " +
			"'admin.coupons.detail.heading', 'admin.coupons.detail.immutable.mid', 'admin.coupons.detail.immutable.strong', 'admin.coupons.detail.immutable.strong_keep', " +
			"'admin.coupons.detail.immutable.tail', 'admin.coupons.detail.meta.current', 'admin.coupons.detail.meta.min', 'admin.coupons.detail.meta.per_user', " +
			"'admin.coupons.detail.meta.used', 'admin.coupons.detail.meta.window', 'admin.coupons.detail.ph.ends_at', 'admin.coupons.detail.ph.min_subtotal', " +
			"'admin.coupons.detail.ph.name', 'admin.coupons.detail.ph.starts_at', 'admin.coupons.detail.submit', 'admin.coupons.detail.time_hint.lead', " +
			"'admin.coupons.detail.time_hint.mid', 'admin.coupons.detail.time_hint.tail', 'admin.coupons.detail.unit_hint.mid', 'admin.coupons.detail.unit_hint.tail', " +
			"'admin.coupons.err_prefix', 'admin.coupons.filter.clear', 'admin.coupons.filter.reset', 'admin.coupons.filter.submit', " +
			"'admin.coupons.heading', 'admin.coupons.hint.anatomy.lead', 'admin.coupons.hint.anatomy.mid', 'admin.coupons.hint.anatomy.mid2', " +
			"'admin.coupons.hint.anatomy.mid3', 'admin.coupons.hint.anatomy.mid4', 'admin.coupons.hint.anatomy.mid5', 'admin.coupons.hint.anatomy.mid6', " +
			"'admin.coupons.hint.anatomy.strong_discount', 'admin.coupons.hint.anatomy.strong_max_uses', 'admin.coupons.hint.anatomy.strong_min', 'admin.coupons.hint.anatomy.strong_per_user', " +
			"'admin.coupons.hint.anatomy.strong_percent', 'admin.coupons.hint.anatomy.strong_window', 'admin.coupons.hint.anatomy.tail', 'admin.coupons.hint.redeem.mid', " +
			"'admin.coupons.hint.redeem.strong_in_txn', 'admin.coupons.hint.redeem.strong_no_button', 'admin.coupons.hint.redeem.tail', 'admin.coupons.list.empty_build_lead', " +
			"'admin.coupons.list.empty_build_tail', 'admin.coupons.list.empty_by_status', 'admin.coupons.list.empty_dash', 'admin.coupons.list.empty_filter_lead', " +
			"'admin.coupons.list.empty_filter_tail', 'admin.coupons.list.empty_no_match_lead', 'admin.coupons.list.empty_no_match_tail', 'admin.coupons.list.empty_no_project', " +
			"'admin.coupons.list.empty_project', 'admin.coupons.list.footer.lead', 'admin.coupons.list.footer.mid', 'admin.coupons.list.footer.mid2', " +
			"'admin.coupons.list.footer.strong_disable', 'admin.coupons.list.footer.strong_unlimited', 'admin.coupons.list.footer.tail', 'admin.coupons.list.heading', " +
			"'admin.coupons.list.total_lead', 'admin.coupons.list.total_tail', 'admin.coupons.no_project.lead', 'admin.coupons.no_project.link', " +
			"'admin.coupons.no_project.tail', 'admin.coupons.ph.ends_at', 'admin.coupons.ph.keyword', 'admin.coupons.ph.max_uses', " +
			"'admin.coupons.ph.per_user_limit', 'admin.coupons.ph.remark', 'admin.coupons.ph.starts_at', 'admin.coupons.redemptions.col.discount', " +
			"'admin.coupons.redemptions.col.order_no', 'admin.coupons.redemptions.col.time', 'admin.coupons.redemptions.col.user', 'admin.coupons.redemptions.empty', " +
			"'admin.coupons.redemptions.heading', 'admin.coupons.redemptions.mid', 'admin.coupons.redemptions.strong_in_txn', 'admin.coupons.redemptions.strong_no_manual', " +
			"'admin.coupons.redemptions.tail', 'admin.coupons.redemptions.total_lead', 'admin.coupons.redemptions.total_mid', 'admin.coupons.redemptions.total_tail', " +
			"'admin.coupons.status.all_option', 'admin.coupons.status.disabled', 'admin.coupons.unit.cent', 'admin.coupons.unit.fixed', " +
			"'admin.coupons.unit.percent', 'admin.customer_detail.action.account_suffix', 'admin.customer_detail.action.unlock', 'admin.customer_detail.actions.disable_mid', " +
			"'admin.customer_detail.actions.disable_strong', 'admin.customer_detail.actions.heading', 'admin.customer_detail.actions.unlock_strong', 'admin.customer_detail.actions.unlock_tail', " +
			"'admin.customer_detail.back', 'admin.customer_detail.back_hint', 'admin.customer_detail.err_prefix', 'admin.customer_detail.field.email', " +
			"'admin.customer_detail.field.last_login', 'admin.customer_detail.field.last_login_ip', 'admin.customer_detail.field.lock_status', 'admin.customer_detail.field.login_failures', " +
			"'admin.customer_detail.field.register_source', 'admin.customer_detail.field.registered_at', 'admin.customer_detail.field.username', 'admin.customer_detail.heading', " +
			"'admin.customer_detail.ip_hint', 'admin.customer_detail.lock.auto', 'admin.customer_detail.lock.failures_lead', 'admin.customer_detail.lock.failures_tail', " +
			"'admin.customer_detail.lock.locked', 'admin.customer_detail.lock.not_locked', 'admin.customer_detail.lock.unlocked', 'admin.customer_detail.lock.until', " +
			"'admin.customer_detail.no_detail', 'admin.customer_detail.orders.count_lead', 'admin.customer_detail.orders.heading', 'admin.customer_detail.orders.last_order', " +
			"'admin.customer_detail.orders.no_order', 'admin.customer_detail.orders.no_projects.lead', 'admin.customer_detail.orders.no_projects.link', 'admin.customer_detail.orders.no_projects.tail', " +
			"'admin.customer_detail.orders.paid_lead', 'admin.customer_detail.orders.paid_tail', 'admin.customer_detail.orders.spend_hint.lead', 'admin.customer_detail.orders.spend_hint.link', " +
			"'admin.customer_detail.orders.spend_hint.mid', 'admin.customer_detail.orders.spend_hint.strong', 'admin.customer_detail.orders.spend_hint.tail', 'admin.customer_detail.orders.spend_lead', " +
			"'admin.customer_detail.orders.summary_failed', 'admin.customer_detail.sentence_period', 'admin.customer_detail.status_locked', 'admin.customers.action.detail', " +
			"'admin.customers.action.unlock', 'admin.customers.badge.active', 'admin.customers.badge.all', 'admin.customers.badge.disabled', " +
			"'admin.customers.badge.locked', 'admin.customers.badge.pending', 'admin.customers.badge.unverified', 'admin.customers.badge.verified', " +
			"'admin.customers.capability_missing', 'admin.customers.col.actions', 'admin.customers.col.customer', 'admin.customers.col.email', " +
			"'admin.customers.col.last_login', 'admin.customers.col.registered_at', 'admin.customers.col.status', 'admin.customers.empty', " +
			"'admin.customers.err_prefix', 'admin.customers.field.registered_at', 'admin.customers.field.registered_to', 'admin.customers.field.username', " +
			"'admin.customers.filter.heading', 'admin.customers.filter.hint.inclusive.lead', 'admin.customers.filter.hint.inclusive.strong', 'admin.customers.filter.hint.inclusive.tail', " +
			"'admin.customers.filter.hint.lead', 'admin.customers.filter.hint.strong', 'admin.customers.filter.hint.tail', 'admin.customers.filter.reset', " +
			"'admin.customers.filter.submit', 'admin.customers.footer.lead', 'admin.customers.footer.mid', 'admin.customers.footer.tail', " +
			"'admin.customers.heading', 'admin.customers.hint.disable.mid', 'admin.customers.hint.disable.strong', 'admin.customers.hint.disable.strong_temp', " +
			"'admin.customers.hint.disable.tail', 'admin.customers.hint.unverified.lead', 'admin.customers.hint.unverified.strong', 'admin.customers.hint.unverified.tail', " +
			"'admin.customers.hint.who.lead', 'admin.customers.hint.who.strong', 'admin.customers.hint.who.tail', 'admin.customers.list.aria', " +
			"'admin.customers.list.heading', 'admin.customers.list.total_lead', 'admin.customers.list.total_tail', 'admin.customers.ph.email_all', " +
			"'admin.customers.ph.keyword', 'admin.customers.ph.status_all', 'admin.customers.status.locked', 'admin.customers.status.locked_until', " +
			"'admin.mail.account_form.encryption', 'admin.mail.account_form.encryption.none', 'admin.mail.account_form.from_email', 'admin.mail.account_form.from_name', " +
			"'admin.mail.account_form.heading', 'admin.mail.account_form.host', 'admin.mail.account_form.name', 'admin.mail.account_form.password', " +
			"'admin.mail.account_form.ph.password', 'admin.mail.account_form.port', 'admin.mail.account_form.purpose', 'admin.mail.account_form.purpose.marketing', " +
			"'admin.mail.account_form.purpose.transactional', 'admin.mail.account_form.rate', 'admin.mail.account_form.reply_to', 'admin.mail.account_form.save', " +
			"'admin.mail.account_form.username', 'admin.mail.accounts.action.set_default', 'admin.mail.accounts.action.test', 'admin.mail.accounts.col.actions', " +
			"'admin.mail.accounts.col.from', 'admin.mail.accounts.col.host', 'admin.mail.accounts.col.last_check', 'admin.mail.accounts.col.name', " +
			"'admin.mail.accounts.col.password', 'admin.mail.accounts.col.purpose', 'admin.mail.accounts.default_badge', 'admin.mail.accounts.empty', " +
			"'admin.mail.accounts.heading', 'admin.mail.accounts.password_set', 'admin.mail.accounts.password_unset', 'admin.mail.accounts.ph.recipient', " +
			"'admin.mail.action.delete', 'admin.mail.automation.action.delete', 'admin.mail.automation.action.enable', 'admin.mail.automation.action.filter', " +
			"'admin.mail.automation.action.pause', 'admin.mail.automation.action.tick', 'admin.mail.automation.back_marketing', 'admin.mail.automation.canvas_link', " +
			"'admin.mail.automation.col.actions', 'admin.mail.automation.col.detail', 'admin.mail.automation.col.email', 'admin.mail.automation.col.name', " +
			"'admin.mail.automation.col.next_run', 'admin.mail.automation.col.node', 'admin.mail.automation.col.status', 'admin.mail.automation.col.trigger', " +
			"'admin.mail.automation.col.trigger_event', 'admin.mail.automation.col.version', 'admin.mail.automation.confirm.delete', 'admin.mail.automation.count.completed', " +
			"'admin.mail.automation.count.failed', 'admin.mail.automation.count.running', 'admin.mail.automation.count.stopped', 'admin.mail.automation.count.waiting', " +
			"'admin.mail.automation.empty', 'admin.mail.automation.filter.all', 'admin.mail.automation.filter.by_id', 'admin.mail.automation.filter.by_status', " +
			"'admin.mail.automation.heading', 'admin.mail.automation.hint.lead', 'admin.mail.automation.hint.mid', 'admin.mail.automation.hint.strong_enable', " +
			"'admin.mail.automation.hint.strong_trigger', 'admin.mail.automation.hint.tail', 'admin.mail.automation.list_heading', 'admin.mail.automation.list_total_lead', " +
			"'admin.mail.automation.list_total_tail', 'admin.mail.automation.new_link', 'admin.mail.automation.run_link', 'admin.mail.automation.runs.empty', " +
			"'admin.mail.automation.runs.heading', 'admin.mail.automation.runs.hint.lead', 'admin.mail.automation.runs.hint.mid', 'admin.mail.automation.runs.hint.strong_failed', " +
			"'admin.mail.automation.runs.hint.strong_waiting', 'admin.mail.automation.runs.hint.tail', 'admin.mail.automation.runs.total_prefix', 'admin.mail.automation.runs.total_suffix', " +
			"'admin.mail.automation.status.active', 'admin.mail.automation.status.draft', 'admin.mail.automation.status.paused', 'admin.mail.automation_canvas.back', " +
			"'admin.mail.automation_canvas.canvas.aria', 'admin.mail.automation_canvas.canvas.empty', 'admin.mail.automation_canvas.field.key', 'admin.mail.automation_canvas.field.next', " +
			"'admin.mail.automation_canvas.field.no', 'admin.mail.automation_canvas.field.param', 'admin.mail.automation_canvas.field.type', 'admin.mail.automation_canvas.field.yes', " +
			"'admin.mail.automation_canvas.hint.lead', 'admin.mail.automation_canvas.hint.strong', 'admin.mail.automation_canvas.hint.tail', 'admin.mail.automation_canvas.kbd.arrow', " +
			"'admin.mail.automation_canvas.kbd.enter', 'admin.mail.automation_canvas.kbd.lead', 'admin.mail.automation_canvas.kbd.move', 'admin.mail.automation_canvas.kbd.shift', " +
			"'admin.mail.automation_canvas.kbd.tab', 'admin.mail.automation_canvas.save', 'admin.mail.automation_canvas.save_hint.lead', 'admin.mail.automation_canvas.save_hint.strong', " +
			"'admin.mail.automation_canvas.save_hint.tail', 'admin.mail.automation_canvas.save_pos', 'admin.mail.automation_canvas.side.hint', 'admin.mail.automation_canvas.use_form', " +
			"'admin.mail.automation_edit.back', 'admin.mail.automation_edit.basic_heading', 'admin.mail.automation_edit.branch.lead', 'admin.mail.automation_edit.branch.mid', " +
			"'admin.mail.automation_edit.branch.strong', 'admin.mail.automation_edit.branch.strong_mail', 'admin.mail.automation_edit.branch.tail', 'admin.mail.automation_edit.canvas_link', " +
			"'admin.mail.automation_edit.col.key', 'admin.mail.automation_edit.col.next', 'admin.mail.automation_edit.col.param', 'admin.mail.automation_edit.col.param_hint', " +
			"'admin.mail.automation_edit.col.type', 'admin.mail.automation_edit.entry_hint', 'admin.mail.automation_edit.field.description', 'admin.mail.automation_edit.field.entry', " +
			"'admin.mail.automation_edit.field.name', 'admin.mail.automation_edit.field.trigger', 'admin.mail.automation_edit.heading_edit', 'admin.mail.automation_edit.heading_new', " +
			"'admin.mail.automation_edit.hint.graph_lead', 'admin.mail.automation_edit.hint.graph_strong', 'admin.mail.automation_edit.hint.graph_tail', 'admin.mail.automation_edit.hint.strong_lead', " +
			"'admin.mail.automation_edit.hint.tail_lead', 'admin.mail.automation_edit.nodes_heading', 'admin.mail.automation_edit.save', 'admin.mail.automation_edit.save_note', " +
			"'admin.mail.automation_edit.templates_hint', 'admin.mail.automation_edit.trigger.lead', 'admin.mail.automation_edit.trigger.manual_tail', 'admin.mail.automation_edit.trigger.strong_manual', " +
			"'admin.mail.automation_edit.trigger.strong_not', 'admin.mail.automation_edit.trigger.tail', 'admin.mail.automation_edit.types.colon', 'admin.mail.automation_edit.types.strong', " +
			"'admin.mail.automation_run.back', 'admin.mail.automation_run.basic_heading', 'admin.mail.automation_run.col.detail', 'admin.mail.automation_run.col.node', " +
			"'admin.mail.automation_run.col.result', 'admin.mail.automation_run.col.time', 'admin.mail.automation_run.col.type', 'admin.mail.automation_run.heading', " +
			"'admin.mail.automation_run.progress.done', 'admin.mail.automation_run.progress.mid', 'admin.mail.automation_run.progress.tail', 'admin.mail.automation_run.row.automation', " +
			"'admin.mail.automation_run.row.contact', 'admin.mail.automation_run.row.error', 'admin.mail.automation_run.row.next_run', 'admin.mail.automation_run.row.node', " +
			"'admin.mail.automation_run.row.progress', 'admin.mail.automation_run.row.span', 'admin.mail.automation_run.row.status', 'admin.mail.automation_run.row.trigger', " +
			"'admin.mail.automation_run.timeline.empty', 'admin.mail.automation_run.timeline.heading', 'admin.mail.automation_run.timeline.lead', 'admin.mail.automation_run.timeline.strong', " +
			"'admin.mail.automation_run.timeline.tail', 'admin.mail.campaign.back', 'admin.mail.campaign.col.clicked', 'admin.mail.campaign.col.contacts', " +
			"'admin.mail.campaign.col.email', 'admin.mail.campaign.col.error', 'admin.mail.campaign.col.link', 'admin.mail.campaign.col.metric', " +
			"'admin.mail.campaign.col.name', 'admin.mail.campaign.col.note', 'admin.mail.campaign.col.opened', 'admin.mail.campaign.col.sent_at', " +
			"'admin.mail.campaign.col.status', 'admin.mail.campaign.col.total_clicks', 'admin.mail.campaign.col.value', 'admin.mail.campaign.delivery_heading', " +
			"'admin.mail.campaign.estimate.metrics', 'admin.mail.campaign.estimate.metrics_tail', 'admin.mail.campaign.estimate.strong', 'admin.mail.campaign.estimate.tail', " +
			"'admin.mail.campaign.links_empty', 'admin.mail.campaign.links_heading', 'admin.mail.campaign.links_sort_note.lead', 'admin.mail.campaign.links_sort_note.strong', " +
			"'admin.mail.campaign.links_sort_note.tail', 'admin.mail.campaign.metric.bounced', 'admin.mail.campaign.metric.bounced_note', 'admin.mail.campaign.metric.click_events', " +
			"'admin.mail.campaign.metric.click_events_note', 'admin.mail.campaign.metric.clicked', 'admin.mail.campaign.metric.clicked_note', 'admin.mail.campaign.metric.complained', " +
			"'admin.mail.campaign.metric.complained_note', 'admin.mail.campaign.metric.failed', 'admin.mail.campaign.metric.failed_note', 'admin.mail.campaign.metric.open_events', " +
			"'admin.mail.campaign.metric.open_events_note', 'admin.mail.campaign.metric.opened', 'admin.mail.campaign.metric.opened_note', 'admin.mail.campaign.metric.opened_note_tail', " +
			"'admin.mail.campaign.metric.sent', 'admin.mail.campaign.metric.sent_note', 'admin.mail.campaign.metric.target', 'admin.mail.campaign.metric.target_note', " +
			"'admin.mail.campaign.metric.unsubscribed', 'admin.mail.campaign.metric.unsubscribed_note', 'admin.mail.campaign.page_prefix', 'admin.mail.campaign.page_suffix', " +
			"'admin.mail.campaign.recipients_empty', 'admin.mail.campaign.recipients_heading', 'admin.mail.campaign.recipients_total_lead', 'admin.mail.campaign.recipients_total_tail', " +
			"'admin.mail.campaign.status_label', 'admin.mail.campaign.subject_label', 'admin.mail.err_prefix', 'admin.mail.heading', " +
			"'admin.mail.hint.purpose.lead', 'admin.mail.hint.purpose.strong', 'admin.mail.hint.purpose.tail', 'admin.mail.hint.secret.lead', " +
			"'admin.mail.hint.secret.mid', 'admin.mail.hint.secret.mid2', 'admin.mail.hint.secret.mid3', 'admin.mail.hint.secret.strong_auth', " +
			"'admin.mail.hint.secret.strong_conn', 'admin.mail.hint.secret.strong_encrypted', 'admin.mail.hint.secret.strong_keep', 'admin.mail.hint.secret.tail', " +
			"'admin.mail.marketing.action.filter', 'admin.mail.marketing.action.update', 'admin.mail.marketing.campaign_form.account', 'admin.mail.marketing.campaign_form.name', " +
			"'admin.mail.marketing.campaign_form.save', 'admin.mail.marketing.campaign_form.subject', 'admin.mail.marketing.campaign_form.target_tags', 'admin.mail.marketing.campaign_form.template', " +
			"'admin.mail.marketing.campaign_status.failed', 'admin.mail.marketing.campaign_status.sending', 'admin.mail.marketing.campaign_status.sent', 'admin.mail.marketing.campaigns.all_subscribed', " +
			"'admin.mail.marketing.campaigns.col.actions', 'admin.mail.marketing.campaigns.col.name', 'admin.mail.marketing.campaigns.col.progress', 'admin.mail.marketing.campaigns.col.status', " +
			"'admin.mail.marketing.campaigns.col.target_tags', 'admin.mail.marketing.campaigns.empty', 'admin.mail.marketing.campaigns.heading', 'admin.mail.marketing.campaigns.report', " +
			"'admin.mail.marketing.campaigns.start', 'admin.mail.marketing.contact_status.subscribe', 'admin.mail.marketing.contact_status.unsubscribe', 'admin.mail.marketing.contacts.col.actions', " +
			"'admin.mail.marketing.contacts.col.consent_source', 'admin.mail.marketing.contacts.col.email', 'admin.mail.marketing.contacts.col.name', 'admin.mail.marketing.contacts.col.source', " +
			"'admin.mail.marketing.contacts.col.status', 'admin.mail.marketing.contacts.col.tags', 'admin.mail.marketing.contacts.empty', 'admin.mail.marketing.contacts.heading', " +
			"'admin.mail.marketing.contacts.total_lead', 'admin.mail.marketing.contacts.total_tail', 'admin.mail.marketing.err_prefix', 'admin.mail.marketing.heading', " +
			"'admin.mail.marketing.hint.bounce.lead', 'admin.mail.marketing.hint.bounce.strong', 'admin.mail.marketing.hint.bounce.tail', 'admin.mail.marketing.hint.consent.lead', " +
			"'admin.mail.marketing.hint.consent.mid', 'admin.mail.marketing.hint.consent.strong', 'admin.mail.marketing.hint.consent.strong_trace', 'admin.mail.marketing.hint.consent.tail', " +
			"'admin.mail.marketing.import.consent.lead', 'admin.mail.marketing.import.consent.strong', 'admin.mail.marketing.import.consent.tail', 'admin.mail.marketing.import.consent_source', " +
			"'admin.mail.marketing.import.content', 'admin.mail.marketing.import.heading', 'admin.mail.marketing.import.hint', 'admin.mail.marketing.import.ph.consent_source', " +
			"'admin.mail.marketing.import.submit', 'admin.mail.marketing.import.tags', 'admin.mail.marketing.import.update_existing.lead', 'admin.mail.marketing.import.update_existing.strong', " +
			"'admin.mail.marketing.import.update_existing.tail', 'admin.mail.marketing.new_campaign.heading', 'admin.mail.marketing.ph.keyword', 'admin.mail.marketing.ph.note', " +
			"'admin.mail.marketing.progress.failed', 'admin.mail.marketing.progress.sent', 'admin.mail.marketing.progress.target', 'admin.mail.marketing.related', " +
			"'admin.mail.marketing.related.automation', 'admin.mail.marketing.related.mail', 'admin.mail.marketing.start_hint.lead', 'admin.mail.marketing.start_hint.strong', " +
			"'admin.mail.marketing.start_hint.tail', 'admin.mail.marketing.status.all', 'admin.mail.marketing.status.bounced', 'admin.mail.marketing.status.complained', " +
			"'admin.mail.marketing.status.pending', 'admin.mail.marketing.status.subscribed', 'admin.mail.marketing.status.unsubscribed', 'admin.mail.marketing.suppression.lead', " +
			"'admin.mail.marketing.suppression.strong', 'admin.mail.marketing.suppression.tail', 'admin.mail.ok_done', 'admin.mail.related', " +
			"'admin.mail.related.automation', 'admin.mail.related.marketing', 'admin.mail.template_form.body_html', 'admin.mail.template_form.body_text', " +
			"'admin.mail.template_form.heading', 'admin.mail.template_form.key', 'admin.mail.template_form.locale', 'admin.mail.template_form.name', " +
			"'admin.mail.template_form.ph.locale', 'admin.mail.template_form.save', 'admin.mail.template_form.subject', 'admin.mail.template_form.variables', " +
			"'admin.mail.templates.col.actions', 'admin.mail.templates.col.key', 'admin.mail.templates.col.locale', 'admin.mail.templates.col.name', " +
			"'admin.mail.templates.col.subject', 'admin.mail.templates.col.variables', 'admin.mail.templates.empty', 'admin.mail.templates.heading', " +
			"'admin.mail.templates.hint.lead', 'admin.mail.templates.hint.mid', 'admin.mail.templates.hint.strong', 'admin.mail.templates.hint.tail', " +
			"'admin.mail.templates.hint.tail2', 'admin.mail.templates.locale_common', 'admin.orders.action.cancel', 'admin.orders.action.collapse', " +
			"'admin.orders.action.refund', 'admin.orders.action.save_note', 'admin.orders.action.transition', 'admin.orders.action.view', " +
			"'admin.orders.actions.heading', 'admin.orders.col.actions', 'admin.orders.col.amount', 'admin.orders.col.created_at', " +
			"'admin.orders.col.customer', 'admin.orders.col.line_discount', 'admin.orders.col.line_subtotal', 'admin.orders.col.line_tax', " +
			"'admin.orders.col.line_total', 'admin.orders.col.order_no', 'admin.orders.col.payment', 'admin.orders.col.product', " +
			"'admin.orders.col.quantity', 'admin.orders.col.status', 'admin.orders.col.time', 'admin.orders.col.unit_price', " +
			"'admin.orders.col.variant', 'admin.orders.detail.admin_note', 'admin.orders.detail.amount', 'admin.orders.detail.amount_total', " +
			"'admin.orders.detail.billing_address', 'admin.orders.detail.breakdown.close', 'admin.orders.detail.breakdown.discount', 'admin.orders.detail.breakdown.shipping', " +
			"'admin.orders.detail.breakdown.subtotal', 'admin.orders.detail.breakdown.tax', 'admin.orders.detail.cancel_reason', 'admin.orders.detail.completed_at', " +
			"'admin.orders.detail.created_at', 'admin.orders.detail.customer', 'admin.orders.detail.customer_note', 'admin.orders.detail.heading', " +
			"'admin.orders.detail.not_filled', 'admin.orders.detail.paid_at', 'admin.orders.detail.pay', 'admin.orders.detail.pay_txn', " +
			"'admin.orders.detail.shipping_address', 'admin.orders.detail.via', 'admin.orders.err_prefix', 'admin.orders.filter.reset', " +
			"'admin.orders.filter.submit', 'admin.orders.heading', 'admin.orders.hint.cancel_refund.lead', 'admin.orders.hint.cancel_refund.strong', " +
			"'admin.orders.hint.cancel_refund.tail', 'admin.orders.hint.flow.lead', 'admin.orders.hint.flow.mid', 'admin.orders.hint.flow.mid2', " +
			"'admin.orders.hint.flow.strong_edges', 'admin.orders.hint.flow.strong_refund', 'admin.orders.hint.flow.strong_server', 'admin.orders.hint.flow.tail', " +
			"'admin.orders.hint.snapshot.lead', 'admin.orders.hint.snapshot.mid', 'admin.orders.hint.snapshot.strong', 'admin.orders.hint.snapshot.strong_cent', " +
			"'admin.orders.hint.snapshot.tail', 'admin.orders.items.empty', 'admin.orders.items.heading', 'admin.orders.items.snapshot_hint.lead', " +
			"'admin.orders.items.snapshot_hint.strong', 'admin.orders.items.snapshot_hint.tail', 'admin.orders.list.empty_no_project', 'admin.orders.list.empty_project', " +
			"'admin.orders.list.empty_tail', 'admin.orders.list.heading', 'admin.orders.list.total_lead', 'admin.orders.list.total_tail', " +
			"'admin.orders.logs.col.from', 'admin.orders.logs.col.operator', 'admin.orders.logs.col.operator_type', 'admin.orders.logs.col.remark', " +
			"'admin.orders.logs.col.to', 'admin.orders.logs.empty', 'admin.orders.logs.heading', 'admin.orders.no_project.lead', " +
			"'admin.orders.no_project.link', 'admin.orders.no_project.tail', 'admin.orders.no_transition', 'admin.orders.note.heading', " +
			"'admin.orders.note.hint.lead', 'admin.orders.note.hint.mid', 'admin.orders.note.hint.strong_judgement', 'admin.orders.note.hint.strong_state', " +
			"'admin.orders.note.hint.tail', 'admin.orders.ph.admin_note', 'admin.orders.ph.cancel_reason', 'admin.orders.ph.keyword', " +
			"'admin.orders.ph.payment', 'admin.orders.ph.refund_reason', 'admin.orders.ph.transition_remark', 'admin.orders.ph.txn_optional', " +
			"'admin.orders.status.all_option', 'admin.orders.status.heading', 'admin.orders.status.hint', 'admin.returns.action.approve', " +
			"'admin.returns.action.collapse', 'admin.returns.action.receive', 'admin.returns.action.reject', 'admin.returns.action.retry_refund', " +
			"'admin.returns.action.view', 'admin.returns.actions.heading', 'admin.returns.auto_receive', 'admin.returns.col.actions', " +
			"'admin.returns.col.created_at', 'admin.returns.col.customer', 'admin.returns.col.line_refund', 'admin.returns.col.order_no', " +
			"'admin.returns.col.product', 'admin.returns.col.quantity', 'admin.returns.col.received_quantity', 'admin.returns.col.refund_amount', " +
			"'admin.returns.col.return_no', 'admin.returns.col.returnable', 'admin.returns.col.status', 'admin.returns.col.unit_price', " +
			"'admin.returns.col.variant', 'admin.returns.detail.admin_note', 'admin.returns.detail.created_at', 'admin.returns.detail.customer', " +
			"'admin.returns.detail.heading', 'admin.returns.detail.order', 'admin.returns.detail.reason', 'admin.returns.detail.received_at', " +
			"'admin.returns.detail.refund_total', 'admin.returns.detail.refunded_at', 'admin.returns.detail.reviewed_at', 'admin.returns.detail.reviewer', " +
			"'admin.returns.detail.txn', 'admin.returns.err_prefix', 'admin.returns.filter.heading', 'admin.returns.filter.hint.lead', " +
			"'admin.returns.filter.hint.strong', 'admin.returns.filter.hint.tail', 'admin.returns.filter.reset', 'admin.returns.filter.submit', " +
			"'admin.returns.heading', 'admin.returns.hint.flow.lead', 'admin.returns.hint.flow.mid', 'admin.returns.hint.flow.mid2', " +
			"'admin.returns.hint.flow.strong_no_refund_only', 'admin.returns.hint.flow.strong_path', 'admin.returns.hint.flow.strong_refund_after', 'admin.returns.hint.flow.tail', " +
			"'admin.returns.hint.status.lead', 'admin.returns.hint.status.strong_auto', 'admin.returns.hint.status.tail', 'admin.returns.items.empty', " +
			"'admin.returns.items.heading', 'admin.returns.items.returnable_hint.lead', 'admin.returns.items.returnable_hint.strong', 'admin.returns.items.returnable_hint.tail', " +
			"'admin.returns.list.empty_no_project', 'admin.returns.list.empty_project', 'admin.returns.list.empty_tail', 'admin.returns.list.heading', " +
			"'admin.returns.list.order_filter_clear', 'admin.returns.list.order_filter_lead', 'admin.returns.list.order_filter_mid', 'admin.returns.list.order_filter_tail', " +
			"'admin.returns.list.status_filter_lead', 'admin.returns.list.status_filter_tail', 'admin.returns.list.total_lead', 'admin.returns.list.total_tail', " +
			"'admin.returns.no_project.lead', 'admin.returns.no_project.link', 'admin.returns.no_project.tail', 'admin.returns.order_filter.clear', " +
			"'admin.returns.order_filter.lead', 'admin.returns.order_filter.mid', 'admin.returns.order_filter.strong_id', 'admin.returns.order_summary.created_at', " +
			"'admin.returns.order_summary.customer', 'admin.returns.order_summary.heading', 'admin.returns.order_summary.missing', 'admin.returns.order_summary.ship_address', " +
			"'admin.returns.order_summary.ship_to', 'admin.returns.order_summary.total', 'admin.returns.ph.keyword', 'admin.returns.ph.reject_remark', " +
			"'admin.returns.ph.remark_optional', 'admin.returns.ph.remark_short', 'admin.returns.ph.review_remark', 'admin.returns.ph.txn_optional', " +
			"'admin.returns.retry_refund.hint.lead', 'admin.returns.retry_refund.hint.strong', 'admin.returns.retry_refund.hint.tail', 'admin.returns.status.all_option', " +
			"'admin.returns.warehouse.default')",
		SQL: mustSQL("190_i18n_seed_marketing.sql"),
	})
	// 191：商品库存类后台模板文案（审计 I18N-001 组D）
	// 判据按本批自己的 key 枚举计数（582 个全列）—— 用总量会被其它批次满足而静默跳过。
	registerSeed(Seed{
		Version:   "191-i18n-seed-product-inventory",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 582 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.inventory.change.boldDict', 'admin.inventory.change.boldSource', 'admin.inventory.change.boldTarget', 'admin.inventory.change.lead', " +
			"'admin.inventory.change.mid', 'admin.inventory.change.mid2', 'admin.inventory.change.optionPick', 'admin.inventory.change.optionReason', " +
			"'admin.inventory.change.optionWarehouse', 'admin.inventory.change.phQty', 'admin.inventory.change.phRemark', 'admin.inventory.change.phSourceRef', " +
			"'admin.inventory.change.phSourceType', 'admin.inventory.change.submit', 'admin.inventory.change.tail', 'admin.inventory.change.title', " +
			"'admin.inventory.change.warehouseDefaultSuffix', 'admin.inventory.change.warehouseSuffixLead', 'admin.inventory.change.warehouseSuffixTail', 'admin.inventory.create.hint', " +
			"'admin.inventory.create.submit', 'admin.inventory.create.title', 'admin.inventory.defaultWarehouse', 'admin.inventory.label.code', " +
			"'admin.inventory.label.name', 'admin.inventory.lastError', 'admin.inventory.lastOk', 'admin.inventory.list.delete', " +
			"'admin.inventory.list.deleteConfirm', 'admin.inventory.list.empty', 'admin.inventory.list.save', 'admin.inventory.list.sortLead', " +
			"'admin.inventory.list.titleLead', 'admin.inventory.list.titleTail', 'admin.inventory.moves.col.delta', 'admin.inventory.moves.col.direction', " +
			"'admin.inventory.moves.col.operator', 'admin.inventory.moves.col.qtyChange', 'admin.inventory.moves.col.reason', 'admin.inventory.moves.col.sourceRef', " +
			"'admin.inventory.moves.col.time', 'admin.inventory.moves.col.warehouse', 'admin.inventory.moves.empty', 'admin.inventory.moves.hint', " +
			"'admin.inventory.moves.titleLead', 'admin.inventory.moves.titleTail', 'admin.inventory.ph.code', 'admin.inventory.ph.name', " +
			"'admin.inventory.ph.sku', 'admin.inventory.ph.sort', 'admin.inventory.reasons.builtin', 'admin.inventory.reasons.col.direction', " +
			"'admin.inventory.reasons.col.name', 'admin.inventory.reasons.col.origin', 'admin.inventory.reasons.col.state', 'admin.inventory.reasons.create', " +
			"'admin.inventory.reasons.custom', 'admin.inventory.reasons.empty', 'admin.inventory.reasons.hintLead', 'admin.inventory.reasons.hintTail', " +
			"'admin.inventory.reasons.phCode', 'admin.inventory.reasons.phName', 'admin.inventory.reasons.titleLead', 'admin.inventory.reasons.titleTail', " +
			"'admin.inventory.setDefault', 'admin.inventory.sku.col.code', 'admin.inventory.sku.col.qty', 'admin.inventory.sku.col.updatedAt', " +
			"'admin.inventory.sku.col.warehouse', 'admin.inventory.sku.empty', 'admin.inventory.sku.notFoundLead', 'admin.inventory.sku.notFoundTail', " +
			"'admin.inventory.sku.optionPick', 'admin.inventory.sku.submit', 'admin.inventory.sku.title', 'admin.inventory.source.bold', " +
			"'admin.inventory.source.lead', 'admin.inventory.source.tail', 'admin.inventory.sourcesHint.lead', 'admin.inventory.sourcesHint.link', " +
			"'admin.inventory.sourcesHint.tail', 'admin.inventory.status.active', 'admin.inventory.status.disabled', 'admin.inventory.title', " +
			"'admin.inventory.warehouse.bold', 'admin.inventory.warehouse.lead', 'admin.inventory.warehouse.mid', 'admin.inventory.warehouse.mid2', " +
			"'admin.inventory_purchases.col.costPrice', 'admin.inventory_purchases.col.doc', 'admin.inventory_purchases.col.kind', 'admin.inventory_purchases.col.operator', " +
			"'admin.inventory_purchases.col.order', 'admin.inventory_purchases.col.outstanding', 'admin.inventory_purchases.col.qty', 'admin.inventory_purchases.col.qtySum', " +
			"'admin.inventory_purchases.col.receipt', 'admin.inventory_purchases.col.received', 'admin.inventory_purchases.col.source', 'admin.inventory_purchases.col.time', " +
			"'admin.inventory_purchases.col.unitPrice', 'admin.inventory_purchases.col.warehouse', 'admin.inventory_purchases.create.linesHint', 'admin.inventory_purchases.create.optionSku', " +
			"'admin.inventory_purchases.create.optionSource', 'admin.inventory_purchases.create.optionWarehouse', 'admin.inventory_purchases.create.submit', 'admin.inventory_purchases.create.title', " +
			"'admin.inventory_purchases.detail.operatorLead', 'admin.inventory_purchases.detail.orderedLead', 'admin.inventory_purchases.detail.remarkLead', 'admin.inventory_purchases.detail.tail', " +
			"'admin.inventory_purchases.filter.optionSourceAll', 'admin.inventory_purchases.filter.optionStatusAll', 'admin.inventory_purchases.filter.phKeyword', 'admin.inventory_purchases.filter.submit', " +
			"'admin.inventory_purchases.history.bold', 'admin.inventory_purchases.history.costNotWritten', 'admin.inventory_purchases.history.costWritten', 'admin.inventory_purchases.history.empty', " +
			"'admin.inventory_purchases.history.lead', 'admin.inventory_purchases.history.optionAllSku', 'admin.inventory_purchases.history.submit', 'admin.inventory_purchases.history.tail', " +
			"'admin.inventory_purchases.history.title', 'admin.inventory_purchases.intro.boldOrder', 'admin.inventory_purchases.intro.boldReceived', 'admin.inventory_purchases.intro.mid', " +
			"'admin.inventory_purchases.intro.tail', 'admin.inventory_purchases.lastError', 'admin.inventory_purchases.lastOk', 'admin.inventory_purchases.list.empty', " +
			"'admin.inventory_purchases.list.receivedLead', 'admin.inventory_purchases.list.titleLead', 'admin.inventory_purchases.list.titleTail', 'admin.inventory_purchases.noInternal.lead', " +
			"'admin.inventory_purchases.noInternal.tail', 'admin.inventory_purchases.noSources.lead', 'admin.inventory_purchases.noSources.link', 'admin.inventory_purchases.noSources.tail', " +
			"'admin.inventory_purchases.ph.code', 'admin.inventory_purchases.ph.lineQty', 'admin.inventory_purchases.ph.lineUnitPrice', 'admin.inventory_purchases.ph.remark', " +
			"'admin.inventory_purchases.production.boldInternal', 'admin.inventory_purchases.production.boldManual', 'admin.inventory_purchases.production.boldNoOrder', 'admin.inventory_purchases.production.lead', " +
			"'admin.inventory_purchases.production.mid', 'admin.inventory_purchases.production.mid2', 'admin.inventory_purchases.production.optionSource', 'admin.inventory_purchases.production.optionWarehouse', " +
			"'admin.inventory_purchases.production.phQty', 'admin.inventory_purchases.production.phRemark', 'admin.inventory_purchases.production.phUnitCost', 'admin.inventory_purchases.production.submit', " +
			"'admin.inventory_purchases.production.tail', 'admin.inventory_purchases.production.title', 'admin.inventory_purchases.receipt.boldContract', 'admin.inventory_purchases.receipt.boldCost', " +
			"'admin.inventory_purchases.receipt.boldIdem', 'admin.inventory_purchases.receipt.full', 'admin.inventory_purchases.receipt.lead', 'admin.inventory_purchases.receipt.mid', " +
			"'admin.inventory_purchases.receipt.mid2', 'admin.inventory_purchases.receipt.phQty', 'admin.inventory_purchases.receipt.phUnitPrice', 'admin.inventory_purchases.receipt.tail', " +
			"'admin.inventory_purchases.title', 'admin.inventory_sources.create.hint.bold', 'admin.inventory_sources.create.hint.lead', 'admin.inventory_sources.create.hint.tail', " +
			"'admin.inventory_sources.create.submit', 'admin.inventory_sources.create.title', 'admin.inventory_sources.filter.allWithDisabled', 'admin.inventory_sources.filter.hint', " +
			"'admin.inventory_sources.filter.onlyRelated', 'admin.inventory_sources.filter.onlyUnrelated', 'admin.inventory_sources.filter.optionRelatedAll', 'admin.inventory_sources.filter.optionStatusActive', " +
			"'admin.inventory_sources.filter.optionTypeAll', 'admin.inventory_sources.filter.phKeyword', 'admin.inventory_sources.filter.submit', 'admin.inventory_sources.filter.title', " +
			"'admin.inventory_sources.intro.boldAll', 'admin.inventory_sources.intro.boldKind', 'admin.inventory_sources.intro.boldRelated', 'admin.inventory_sources.intro.lead', " +
			"'admin.inventory_sources.intro.mid1', 'admin.inventory_sources.intro.mid2', 'admin.inventory_sources.intro.tail', 'admin.inventory_sources.intro2.bold', " +
			"'admin.inventory_sources.intro2.lead', 'admin.inventory_sources.intro2.tail', 'admin.inventory_sources.label.code', 'admin.inventory_sources.label.name', " +
			"'admin.inventory_sources.lastError', 'admin.inventory_sources.lastOk', 'admin.inventory_sources.list.delete', 'admin.inventory_sources.list.deleteConfirm', " +
			"'admin.inventory_sources.list.empty', 'admin.inventory_sources.list.save', 'admin.inventory_sources.list.settlePriceLead', 'admin.inventory_sources.list.titleLead', " +
			"'admin.inventory_sources.list.titleTail', 'admin.inventory_sources.ph.code', 'admin.inventory_sources.ph.config', 'admin.inventory_sources.ph.name', " +
			"'admin.inventory_sources.ph.settlePrice', 'admin.inventory_sources.ph.sort', 'admin.inventory_sources.stats.col.count', 'admin.inventory_sources.stats.col.related', " +
			"'admin.inventory_sources.stats.col.type', 'admin.inventory_sources.stats.external', 'admin.inventory_sources.stats.hint', 'admin.inventory_sources.stats.internal', " +
			"'admin.inventory_sources.stats.related', 'admin.inventory_sources.stats.settlePriced', 'admin.inventory_sources.stats.title', 'admin.inventory_sources.stats.total', " +
			"'admin.inventory_sources.stats.unrelated', 'admin.inventory_sources.title', 'admin.inventory_sources.update.optionKeep', 'admin.inventory_sources.update.phConfig', " +
			"'admin.inventory_sources.update.phSettlePrice', 'admin.product_attributes.create.submit', 'admin.product_attributes.create.title', 'admin.product_attributes.intro', " +
			"'admin.product_attributes.isVariation', 'admin.product_attributes.lastError', 'admin.product_attributes.list.delete', 'admin.product_attributes.list.deleteConfirm', " +
			"'admin.product_attributes.list.empty', 'admin.product_attributes.list.save', 'admin.product_attributes.list.saveValues', 'admin.product_attributes.list.title', " +
			"'admin.product_attributes.list.valueCountTail', 'admin.product_attributes.notVariation', 'admin.product_attributes.ph.groupName', 'admin.product_attributes.ph.groupNameExample', " +
			"'admin.product_attributes.ph.key', 'admin.product_attributes.ph.keyExample', 'admin.product_attributes.ph.sort', 'admin.product_attributes.title', " +
			"'admin.product_brands.create.submit', 'admin.product_brands.create.title', 'admin.product_brands.intro', 'admin.product_brands.label.description', " +
			"'admin.product_brands.label.logo', 'admin.product_brands.label.name', 'admin.product_brands.label.seoDesc', 'admin.product_brands.label.seoTitle', " +
			"'admin.product_brands.label.slug', 'admin.product_brands.lastError', 'admin.product_brands.list.delete', 'admin.product_brands.list.deleteConfirm', " +
			"'admin.product_brands.list.empty', 'admin.product_brands.list.save', 'admin.product_brands.list.sortLead', 'admin.product_brands.list.title', " +
			"'admin.product_brands.ph.description', 'admin.product_brands.ph.logo', 'admin.product_brands.ph.nameExample', 'admin.product_brands.ph.seoDesc', " +
			"'admin.product_brands.ph.seoTitle', 'admin.product_brands.ph.slug', 'admin.product_brands.ph.sort', 'admin.product_brands.projectAria', " +
			"'admin.product_brands.seoScore', 'admin.product_brands.title', 'admin.product_bundle.cfg.col.available', 'admin.product_bundle.cfg.col.defaultQty', " +
			"'admin.product_bundle.cfg.col.maxQty', 'admin.product_bundle.cfg.col.minQty', 'admin.product_bundle.cfg.col.required', 'admin.product_bundle.cfg.maxOptions', " +
			"'admin.product_bundle.cfg.maxTotalQty', 'admin.product_bundle.cfg.minTotalQty', 'admin.product_bundle.cfg.save', 'admin.product_bundle.detailFailed', " +
			"'admin.product_bundle.intro.end', 'admin.product_bundle.intro.lead', 'admin.product_bundle.intro.mid', 'admin.product_bundle.intro.skuBold', " +
			"'admin.product_bundle.intro.sourceBold', 'admin.product_bundle.intro.tail', 'admin.product_bundle.intro.totalBold', 'admin.product_bundle.option.optional', " +
			"'admin.product_bundle.option.required', 'admin.product_bundle.preview.hint', 'admin.product_bundle.preview.title', 'admin.product_bundle.rule.basePriceLead', " +
			"'admin.product_bundle.rule.basePriceTail', 'admin.product_bundle.rule.title', 'admin.product_bundle.saveError', 'admin.product_bundle.selectProductPlaceholder', " +
			"'admin.product_bundle.skuCountLead', 'admin.product_bundle.skuCountTail', 'admin.product_bundle.title', 'admin.product_categories.create.submit', " +
			"'admin.product_categories.create.title', 'admin.product_categories.intro', 'admin.product_categories.label.description', 'admin.product_categories.label.image', " +
			"'admin.product_categories.label.name', 'admin.product_categories.label.parent', 'admin.product_categories.label.seoDesc', 'admin.product_categories.label.seoTitle', " +
			"'admin.product_categories.label.slug', 'admin.product_categories.lastError', 'admin.product_categories.list.delete', 'admin.product_categories.list.deleteConfirm', " +
			"'admin.product_categories.list.empty', 'admin.product_categories.list.save', 'admin.product_categories.list.sortLead', 'admin.product_categories.list.title', " +
			"'admin.product_categories.option.topLevel', 'admin.product_categories.ph.description', 'admin.product_categories.ph.image', 'admin.product_categories.ph.nameExample', " +
			"'admin.product_categories.ph.seoDesc', 'admin.product_categories.ph.seoTitle', 'admin.product_categories.ph.slug', 'admin.product_categories.ph.sort', " +
			"'admin.product_categories.projectAria', 'admin.product_categories.seoScore', 'admin.product_categories.title', 'admin.product_detail_template.afterDefaultId', " +
			"'admin.product_detail_template.applySubmit', 'admin.product_detail_template.backToList', 'admin.product_detail_template.backToListArrow', 'admin.product_detail_template.boundToInstance', " +
			"'admin.product_detail_template.changeURLHint', 'admin.product_detail_template.changeURLSubmit', 'admin.product_detail_template.currentTemplateLead', 'admin.product_detail_template.defaultIdLead', " +
			"'admin.product_detail_template.detailSuffix', 'admin.product_detail_template.intro', 'admin.product_detail_template.keepRedirect', 'admin.product_detail_template.label.changeURL', " +
			"'admin.product_detail_template.label.previewTemplate', 'admin.product_detail_template.label.useTemplate', 'admin.product_detail_template.lastError', 'admin.product_detail_template.named.bound', " +
			"'admin.product_detail_template.named.col.name', 'admin.product_detail_template.named.col.state', 'admin.product_detail_template.named.col.templateId', 'admin.product_detail_template.named.col.updatedAt', " +
			"'admin.product_detail_template.named.col.version', 'admin.product_detail_template.named.create', 'admin.product_detail_template.named.empty', 'admin.product_detail_template.named.label.copyFrom', " +
			"'admin.product_detail_template.named.ph.name', 'admin.product_detail_template.named.tableAria', 'admin.product_detail_template.named.titleLead', 'admin.product_detail_template.named.titleTail', " +
			"'admin.product_detail_template.named.typeDefaultLabel', 'admin.product_detail_template.named.visualEdit', 'admin.product_detail_template.notPublished', 'admin.product_detail_template.notReady', " +
			"'admin.product_detail_template.ph.publishURLLead', 'admin.product_detail_template.ph.publishURLTail', 'admin.product_detail_template.previewSubmit', 'admin.product_detail_template.publishSubmit', " +
			"'admin.product_detail_template.published', 'admin.product_detail_template.title', 'admin.product_detail_template.typeDefault', 'admin.product_detail_template.typeDefaultSuffix', " +
			"'admin.product_detail_template.versionLead', 'admin.product_detail_template.versionTail', 'admin.product_pricing.applied.lead', 'admin.product_pricing.applied.tail', " +
			"'admin.product_pricing.apply.confirm', 'admin.product_pricing.apply.hint', 'admin.product_pricing.apply.labelFilter', 'admin.product_pricing.apply.labelRounding', " +
			"'admin.product_pricing.apply.labelRule', 'admin.product_pricing.apply.labelScope', 'admin.product_pricing.apply.phAmount', 'admin.product_pricing.apply.phBrandId', " +
			"'admin.product_pricing.apply.phCategoryId', 'admin.product_pricing.apply.phKeyword', 'admin.product_pricing.apply.phMargin', 'admin.product_pricing.apply.phMultiplier', " +
			"'admin.product_pricing.apply.phNote', 'admin.product_pricing.apply.phStatus', 'admin.product_pricing.apply.phTagId', 'admin.product_pricing.apply.phTargetId', " +
			"'admin.product_pricing.apply.preview', 'admin.product_pricing.apply.roundingAria', 'admin.product_pricing.apply.ruleTypeAria', 'admin.product_pricing.apply.scopeAria', " +
			"'admin.product_pricing.apply.scopeFilter', 'admin.product_pricing.apply.scopeProduct', 'admin.product_pricing.apply.scopeSku', 'admin.product_pricing.apply.submit', " +
			"'admin.product_pricing.apply.title', 'admin.product_pricing.col.costPrice', 'admin.product_pricing.col.desc', 'admin.product_pricing.col.diff', " +
			"'admin.product_pricing.col.newPrice', 'admin.product_pricing.col.oldPrice', 'admin.product_pricing.col.params', 'admin.product_pricing.col.product', " +
			"'admin.product_pricing.col.result', 'admin.product_pricing.col.type', 'admin.product_pricing.history.changedLead', 'admin.product_pricing.history.changedTail', " +
			"'admin.product_pricing.history.empty', 'admin.product_pricing.history.filterLead', 'admin.product_pricing.history.noItems', 'admin.product_pricing.history.noteLead', " +
			"'admin.product_pricing.history.operatorLead', 'admin.product_pricing.history.tableAria', 'admin.product_pricing.history.title', 'admin.product_pricing.intro', " +
			"'admin.product_pricing.lastError', 'admin.product_pricing.preview.confirmHint', 'admin.product_pricing.preview.empty', 'admin.product_pricing.preview.summaryChanged', " +
			"'admin.product_pricing.preview.summaryClose', 'admin.product_pricing.preview.summaryRounding', 'admin.product_pricing.preview.summaryRule', 'admin.product_pricing.preview.summaryScope', " +
			"'admin.product_pricing.preview.summarySkipped', 'admin.product_pricing.preview.summaryTargets', 'admin.product_pricing.preview.summaryUnchanged', 'admin.product_pricing.preview.tableAria', " +
			"'admin.product_pricing.preview.title', 'admin.product_pricing.rounding.hint', 'admin.product_pricing.rules.aria', 'admin.product_pricing.rules.hint', " +
			"'admin.product_pricing.rules.requiresCost', 'admin.product_pricing.rules.title', 'admin.product_pricing.title', 'admin.product_tags.col.desc', " +
			"'admin.product_tags.col.params', 'admin.product_tags.col.product', 'admin.product_tags.col.slug', 'admin.product_tags.col.state', " +
			"'admin.product_tags.col.type', 'admin.product_tags.create.hint', 'admin.product_tags.create.submit', 'admin.product_tags.create.title', " +
			"'admin.product_tags.intro', 'admin.product_tags.kindAria', 'admin.product_tags.kindManual', 'admin.product_tags.kindRule', " +
			"'admin.product_tags.label.kind', 'admin.product_tags.label.name', 'admin.product_tags.label.rule', 'admin.product_tags.label.slug', " +
			"'admin.product_tags.lastError', 'admin.product_tags.list.delete', 'admin.product_tags.list.deleteConfirm', 'admin.product_tags.list.empty', " +
			"'admin.product_tags.list.hitAria', 'admin.product_tags.list.hitEmpty', 'admin.product_tags.list.hitLead', 'admin.product_tags.list.hitMid', " +
			"'admin.product_tags.list.hitTitle', 'admin.product_tags.list.recalc', 'admin.product_tags.list.save', 'admin.product_tags.list.title', " +
			"'admin.product_tags.ph.days', 'admin.product_tags.ph.daysShort', 'admin.product_tags.ph.maxPrice', 'admin.product_tags.ph.minPrice', " +
			"'admin.product_tags.ph.nameExample', 'admin.product_tags.ph.slug', 'admin.product_tags.ph.sort', 'admin.product_tags.projectAria', " +
			"'admin.product_tags.recalcAll', 'admin.product_tags.ruleEmpty', 'admin.product_tags.ruleTypeAria', 'admin.product_tags.rules.aria', " +
			"'admin.product_tags.rules.hint', 'admin.product_tags.rules.title', 'admin.product_tags.title', 'admin.product_translations.col.field', " +
			"'admin.product_translations.col.origin', 'admin.product_translations.col.source', 'admin.product_translations.col.state', 'admin.product_translations.col.target', " +
			"'admin.product_translations.doneLead', 'admin.product_translations.doneTail', 'admin.product_translations.empty', 'admin.product_translations.errorsTitle', " +
			"'admin.product_translations.groupMetaLead', 'admin.product_translations.groupMetaMid', 'admin.product_translations.groupMetaTail', 'admin.product_translations.label.lang', " +
			"'admin.product_translations.label.project', 'admin.product_translations.noscriptSwitch', 'admin.product_translations.ph.target', 'admin.product_translations.richHint', " +
			"'admin.product_translations.saveAll', 'admin.product_translations.saveHint', 'admin.product_translations.scopeLead', 'admin.product_translations.scopeTail', " +
			"'admin.product_translations.state.missing', 'admin.product_translations.state.translated', 'admin.product_translations.title', 'admin.product_translations.viewProject', " +
			"'admin.products.action.delete', 'admin.products.attrs.ph', 'admin.products.attrs.save', 'admin.products.col.comparePrice', " +
			"'admin.products.col.costPrice', 'admin.products.col.enabled', 'admin.products.col.price', 'admin.products.col.spec', " +
			"'admin.products.col.stock', 'admin.products.combo.generateAll', 'admin.products.combo.generateSelected', 'admin.products.combo.hint', " +
			"'admin.products.combo.title', 'admin.products.create.submit', 'admin.products.hint.and', 'admin.products.hint.attrLead', " +
			"'admin.products.hint.attrLink', 'admin.products.hint.attrTail', 'admin.products.hint.brandLink', 'admin.products.hint.brandTail', " +
			"'admin.products.hint.categoryLink', 'admin.products.hint.tagLink', 'admin.products.hint.tagTail', 'admin.products.lastError', " +
			"'admin.products.list.empty', 'admin.products.list.title', 'admin.products.ph.attributeIds', 'admin.products.ph.defaultPrice', " +
			"'admin.products.ph.name', 'admin.products.ph.slug', 'admin.products.rating.add', 'admin.products.rating.close', " +
			"'admin.products.rating.emptyHint', 'admin.products.rating.hasHint', 'admin.products.rating.label', 'admin.products.rating.mid', " +
			"'admin.products.rating.none', 'admin.products.rating.open', 'admin.products.rating.phScore', 'admin.products.ratings.aria', " +
			"'admin.products.ratings.colSource', 'admin.products.ratings.colTime', 'admin.products.row.delete', 'admin.products.row.deleteConfirm', " +
			"'admin.products.row.detailTemplate', 'admin.products.row.preview', 'admin.products.row.price', 'admin.products.row.seoScore', " +
			"'admin.products.row.translations', 'admin.products.row.variantCountSuffix', 'admin.products.row.variationSuffix', 'admin.products.tags.autoTail', " +
			"'admin.products.tags.legendAuto', 'admin.products.tags.legendManual', 'admin.products.tags.manualLead', 'admin.products.tags.manualMid', " +
			"'admin.products.tags.noManualLead', 'admin.products.tags.save', 'admin.products.tags.title', 'admin.products.taxonomy.attachedLead', " +
			"'admin.products.taxonomy.attachedMid', 'admin.products.taxonomy.brandLabel', 'admin.products.taxonomy.brandLead', 'admin.products.taxonomy.legendCategories', " +
			"'admin.products.taxonomy.noCategoriesLead', 'admin.products.taxonomy.noCategoriesTail', 'admin.products.taxonomy.primaryLabel', 'admin.products.taxonomy.save', " +
			"'admin.products.taxonomy.title', 'admin.products.taxonomy.unset', 'admin.products.title', 'admin.products.variant.add', " +
			"'admin.products.variant.phPrice', 'admin.products.variant.phSku', 'admin.products.variant.stockLink', 'admin.products.variants.aria', " +
			"'admin.products.warehouse.defaultOption', 'admin.products.warehouse.label')",
		SQL: mustSQL("191_i18n_seed_product_inventory.sql"),
	})

	// 195：sys_translation 工程作用域（审计 I18N-009）。
	// 判据按**本批自己的列名 + 索引名**枚举计数 —— sys_translation 早已存在，
	// 用默认的「表存在即跳过」必然误跳过；也不可用总量。
	register(Migration{
		Version:   "195-sys-translation-project-scope",
		TableName: "sys_translation",
		CheckSQL: "SELECT CASE WHEN (" +
			"(SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() " +
			"AND table_name = ? AND column_name = 'project_id') + " +
			"(SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
			"AND tablename = 'sys_translation' AND indexname IN " +
			"('uq_sys_translation_scope_key', 'idx_sys_translation_project_hash_lang'))" +
			") >= 3 THEN 1 ELSE 0 END",
		SQL: mustSQL("195_sys_translation_project_scope.sql"),
	})

	// 196：采购单状态由数据库兜底（审计 DB-008）——status 取决于明细行，生成列表达不了
	// 这种跨行推导，改用「明细行 AFTER 重算 + 单头 BEFORE 守卫」两道触发器。
	// 判据按**本批自己的四个对象名**枚举计数（2 个函数 + 2 个触发器）——用总量会被别的批次满足而静默跳过。
	register(Migration{
		Version:   "196-inventory-purchase-status-sync",
		TableName: "inventory_purchase_orders",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM (" +
			"SELECT p.proname AS name FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace " +
			"WHERE p.proname IN ('fn_inventory_purchase_order_status_sync', 'fn_inventory_purchase_order_status_guard') " +
			"AND n.nspname = current_schema() " +
			"UNION ALL " +
			"SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid " +
			"AND c.relname IN (?, 'inventory_purchase_order_lines') " +
			"JOIN pg_namespace n2 ON n2.oid = c.relnamespace " +
			"WHERE t.tgname IN ('trg_inventory_purchase_lines_status_sync', 'trg_inventory_purchase_orders_status_guard') " +
			"AND n2.nspname = current_schema() " +
			"AND NOT t.tgisinternal" +
			") AS batch_196",
		SQL: mustSQL("196_inventory_purchase_status_sync.sql"),
	})

	// 197：后台 HTMX 全局反馈 + 仪表盘数据口径标注的文案词条（审计 UI-011 / UI-012）。
	// 判据按**本批自己的 8 个 key** 枚举计数（lang=zh-CN 下每 key 一行）。
	registerSeed(Seed{
		Version:      "197-i18n-seed-admin-htmx-dashboard",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.dashboard.placeholder.analytics', 'admin.dashboard.placeholder.inventory', 'admin.dashboard.placeholder.lead', 'admin.dashboard.placeholder.links', 'admin.dashboard.placeholder.orders', 'shell.htmx.error', 'shell.htmx.network', 'shell.htmx.timeout')",
		SQL:          mustSQL("197_i18n_seed_admin_htmx_dashboard.sql"),
	})

	// 198：masterdata 模块文案 key 化（审计 CQ-010 收尾）——6 个 Err + 1 个 Msg 的值从中文文案
	// 改成 i18n key（masterdata.err.* / masterdata.msg.*），文案落本表。改值的原因：ErrorAuto 的判据只认
	// 「key 形态」与「enums 常量名形态」两种确定性形态，值本身是中文常量两种都不匹配，会被判成内部错误
	// （500 + 通用文案）。判据按**本批自己的 7 个 key** 枚举计数（lang=zh-CN 下每 key 一行）——
	// 用总量会被别的批次的行满足而静默跳过。
	registerSeed(Seed{
		Version:   "198-masterdata-error-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'masterdata.err.invalidParam', 'masterdata.err.entityTypeInvalid', 'masterdata.err.actionInvalid', " +
			"'masterdata.err.entityIDRequired', 'masterdata.err.projectRequired', 'masterdata.err.timeRangeInvalid', " +
			"'masterdata.msg.listSuccess')",
		SQL: mustSQL("198_masterdata_error_keys.sql"),
	})

	// 192：站点结构类后台模板文案（审计 I18N-001 组E：site_slots / theme / theme_settings /
	// pages / page_translations / blocks / navigations / navigation_translations / menus）。
	// 判据按本批自己的 key 枚举计数（298 个全列）—— 用总量会被其它批次满足而静默跳过。
	registerSeed(Seed{
		Version:   "192-i18n-seed-admin-site-structure",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 298 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.blocks.action.delete', 'admin.blocks.action.edit', 'admin.blocks.blocks_aria', 'admin.blocks.blocks_empty', " +
			"'admin.blocks.blocks_heading', 'admin.blocks.col.actions', 'admin.blocks.col.kind', 'admin.blocks.col.name', " +
			"'admin.blocks.col.reuse', 'admin.blocks.col.updated_at', 'admin.blocks.confirm_delete_prefix', 'admin.blocks.confirm_delete_suffix', " +
			"'admin.blocks.confirm_delete_suffix_bound', 'admin.blocks.create', 'admin.blocks.footers_aria', 'admin.blocks.footers_empty', " +
			"'admin.blocks.footers_heading', 'admin.blocks.headers_aria', 'admin.blocks.headers_empty', 'admin.blocks.headers_heading', " +
			"'admin.blocks.heading', 'admin.blocks.kind.about', 'admin.blocks.kind.announcement', 'admin.blocks.kind.banner', " +
			"'admin.blocks.kind.block', 'admin.blocks.kind.brands', 'admin.blocks.kind.breadcrumb', 'admin.blocks.kind.contact', " +
			"'admin.blocks.kind.cta', 'admin.blocks.kind.drawer', 'admin.blocks.kind.footer', 'admin.blocks.kind.grid', " +
			"'admin.blocks.kind.header', 'admin.blocks.kind.search', 'admin.blocks.kind.sidebar', 'admin.blocks.kind.snippet', " +
			"'admin.blocks.kind.trust', 'admin.blocks.name_placeholder', 'admin.blocks.no_project', 'admin.blocks.notes', " +
			"'admin.blocks.notes_heading', 'admin.blocks.notes_reuse_global', 'admin.blocks.notes_reuse_global_tail', 'admin.blocks.notes_reuse_lead', " +
			"'admin.blocks.notes_reuse_template', 'admin.blocks.notes_reuse_template_tail', 'admin.blocks.reuse.global', 'admin.blocks.reuse.template', " +
			"'admin.menus.action.cancel', 'admin.menus.action.delete', 'admin.menus.action.edit', 'admin.menus.col.actions', " +
			"'admin.menus.col.icon', 'admin.menus.col.path', 'admin.menus.col.remark', 'admin.menus.col.sort', " +
			"'admin.menus.col.status', 'admin.menus.col.title', 'admin.menus.col.type', 'admin.menus.confirm_delete', " +
			"'admin.menus.count_prefix', 'admin.menus.count_suffix', 'admin.menus.create', 'admin.menus.create_drawer_title', " +
			"'admin.menus.create_submit', 'admin.menus.edit_drawer_prefix', 'admin.menus.empty', 'admin.menus.field.icon', " +
			"'admin.menus.field.parent', 'admin.menus.field.path', 'admin.menus.field.path_placeholder', 'admin.menus.field.remark', " +
			"'admin.menus.field.sort', 'admin.menus.field.status', 'admin.menus.field.title', 'admin.menus.field.title_placeholder', " +
			"'admin.menus.field.type', 'admin.menus.filter_placeholder', 'admin.menus.heading', 'admin.menus.parent_root', " +
			"'admin.menus.save_submit', 'admin.menus.status.disabled', 'admin.menus.status.enabled', 'admin.menus.type.button', " +
			"'admin.menus.type.dir', 'admin.menus.type.link', 'admin.menus.type.menu', 'admin.menus.type.unknown', " +
			"'admin.navigation_translations.dedupe_hint', 'admin.navigation_translations.empty_group', 'admin.navigation_translations.has_target', 'admin.navigation_translations.heading', " +
			"'admin.navigation_translations.intro', 'admin.navigation_translations.save', 'admin.navigation_translations.summary_lead', 'admin.navigation_translations.summary_mid', " +
			"'admin.navigation_translations.summary_tail', 'admin.navigation_translations.switch_lang', 'admin.navigation_translations.target_placeholder', 'admin.navigations.action.delete', " +
			"'admin.navigations.action.edit', 'admin.navigations.add', 'admin.navigations.add_heading', 'admin.navigations.add_to_menu', " +
			"'admin.navigations.col.actions', 'admin.navigations.col.item', 'admin.navigations.col.link', 'admin.navigations.col.sort', " +
			"'admin.navigations.col.target', 'admin.navigations.confirm_delete_prefix', 'admin.navigations.confirm_delete_suffix', 'admin.navigations.edit_path_placeholder', " +
			"'admin.navigations.edit_title_placeholder', 'admin.navigations.empty', 'admin.navigations.heading', 'admin.navigations.intro', " +
			"'admin.navigations.kind.footer', 'admin.navigations.kind.header', 'admin.navigations.no_project', 'admin.navigations.no_public_path', " +
			"'admin.navigations.parent.none', 'admin.navigations.parent.prefix', 'admin.navigations.parent.suffix', 'admin.navigations.path_placeholder', " +
			"'admin.navigations.save', 'admin.navigations.source_heading', 'admin.navigations.source_hint', 'admin.navigations.structure_heading', " +
			"'admin.navigations.target.blank', 'admin.navigations.target.self', 'admin.navigations.title_down', 'admin.navigations.title_placeholder', " +
			"'admin.navigations.title_up', 'admin.page_translations.ai_button', 'admin.page_translations.ai_title', 'admin.page_translations.col.field', " +
			"'admin.page_translations.col.origin', 'admin.page_translations.col.source', 'admin.page_translations.col.status', 'admin.page_translations.col.target', " +
			"'admin.page_translations.default_lang_hint', 'admin.page_translations.empty_filtered', 'admin.page_translations.empty_no_text', 'admin.page_translations.filter.ai', " +
			"'admin.page_translations.filter.all', 'admin.page_translations.filter.count_prefix', 'admin.page_translations.filter.count_suffix', 'admin.page_translations.filter.manual', " +
			"'admin.page_translations.filter.missing', 'admin.page_translations.filter_label', 'admin.page_translations.group_count_prefix', 'admin.page_translations.group_count_suffix', " +
			"'admin.page_translations.heading', 'admin.page_translations.lang_label', 'admin.page_translations.limit_lead', 'admin.page_translations.limit_rich', " +
			"'admin.page_translations.limit_unit', 'admin.page_translations.origin_title', 'admin.page_translations.page_progress', 'admin.page_translations.reuse_lead', " +
			"'admin.page_translations.reuse_occurrences_lead', 'admin.page_translations.reuse_occurrences_tail', 'admin.page_translations.reuse_shared_lead', 'admin.page_translations.reuse_shared_mid', " +
			"'admin.page_translations.reuse_shared_tail', 'admin.page_translations.reuse_tail', 'admin.page_translations.rich', 'admin.page_translations.save_all', " +
			"'admin.page_translations.save_hint', 'admin.page_translations.saved_lead', 'admin.page_translations.saved_mid', 'admin.page_translations.saved_tail', " +
			"'admin.page_translations.site_progress_lead', 'admin.page_translations.site_progress_mid', 'admin.page_translations.site_progress_tail', 'admin.page_translations.status.missing', " +
			"'admin.page_translations.status.translated', 'admin.page_translations.switch_lang', 'admin.page_translations.target_placeholder', 'admin.page_translations.unsaved', " +
			"'admin.pages.action.edit', 'admin.pages.action.live', 'admin.pages.action.preview', 'admin.pages.action.translations', " +
			"'admin.pages.blank_page', 'admin.pages.blueprint_title', 'admin.pages.col.actions', 'admin.pages.col.kind', " +
			"'admin.pages.col.path', 'admin.pages.col.status', 'admin.pages.col.updated_at', 'admin.pages.col.version', " +
			"'admin.pages.create_page_heading', 'admin.pages.create_page_submit', 'admin.pages.create_project_heading', 'admin.pages.create_project_submit', " +
			"'admin.pages.empty', 'admin.pages.list_heading', 'admin.pages.no_project', 'admin.pages.path_placeholder', " +
			"'admin.pages.project_name_placeholder', 'admin.pages.status.draft', 'admin.pages.status.published', 'admin.pages.status.staged', " +
			"'admin.pages.status.stale', 'admin.pages.table_aria', 'admin.site_slots.action.unbind', 'admin.site_slots.bound_count', " +
			"'admin.site_slots.col.actions', 'admin.site_slots.col.bound', 'admin.site_slots.col.slot', 'admin.site_slots.col.usage', " +
			"'admin.site_slots.deleted_count', 'admin.site_slots.deleted_page.lead', 'admin.site_slots.deleted_page.strong', 'admin.site_slots.deleted_page.tail', " +
			"'admin.site_slots.draft_path', 'admin.site_slots.err_prefix', 'admin.site_slots.foot.lead', 'admin.site_slots.foot.strong', " +
			"'admin.site_slots.foot.tail', 'admin.site_slots.intro1.lead', 'admin.site_slots.intro1.mid', 'admin.site_slots.intro1.strong', " +
			"'admin.site_slots.intro1.strong2', 'admin.site_slots.intro1.tail', 'admin.site_slots.intro2.lead', 'admin.site_slots.intro2.mid', " +
			"'admin.site_slots.intro2.strong', 'admin.site_slots.intro2.strong2', 'admin.site_slots.intro2.tail', 'admin.site_slots.intro3.mid1', " +
			"'admin.site_slots.intro3.mid2', 'admin.site_slots.intro3.strong', 'admin.site_slots.intro3.strong2', 'admin.site_slots.intro3.strong3', " +
			"'admin.site_slots.intro3.tail', 'admin.site_slots.no_pages.lead', 'admin.site_slots.no_pages.mid', 'admin.site_slots.no_pages.strong', " +
			"'admin.site_slots.no_pages.strong2', 'admin.site_slots.no_pages.tail', 'admin.site_slots.no_pages_inline', 'admin.site_slots.no_project.lead', " +
			"'admin.site_slots.no_project.tail', 'admin.site_slots.no_rows', 'admin.site_slots.overview.hint.lead', 'admin.site_slots.overview.hint.mid', " +
			"'admin.site_slots.overview.hint.strong', 'admin.site_slots.overview.hint.strong2', 'admin.site_slots.overview.hint.tail', 'admin.site_slots.overview.title', " +
			"'admin.site_slots.page_deleted', 'admin.site_slots.pages_link', 'admin.site_slots.pick_project', 'admin.site_slots.public_path', " +
			"'admin.site_slots.select_page', 'admin.site_slots.slots.count_suffix', 'admin.site_slots.slots.title', 'admin.site_slots.title', " +
			"'admin.site_slots.unbound', 'admin.site_slots.unpublished.lead', 'admin.site_slots.unpublished.lead2', 'admin.site_slots.unpublished.strong', " +
			"'admin.site_slots.unpublished.tail', 'admin.site_slots.unpublished.tail2', 'admin.site_slots.unpublished_count', 'admin.theme.action.activate', " +
			"'admin.theme.action.delete', 'admin.theme.action.settings', 'admin.theme.col.actions', 'admin.theme.col.created_at', " +
			"'admin.theme.col.name', 'admin.theme.col.status', 'admin.theme.col.updated_at', 'admin.theme.confirm_delete_prefix', " +
			"'admin.theme.confirm_delete_suffix', 'admin.theme.create', 'admin.theme.empty', 'admin.theme.heading', " +
			"'admin.theme.list_heading', 'admin.theme.name_placeholder', 'admin.theme.no_project', 'admin.theme.notes', " +
			"'admin.theme.notes_heading', 'admin.theme.status.active', 'admin.theme.status.inactive', 'admin.theme.table_aria', " +
			"'admin.theme_settings.announcement_block', 'admin.theme_settings.back', 'admin.theme_settings.color_placeholder', 'admin.theme_settings.footer_block', " +
			"'admin.theme_settings.global_blocks', 'admin.theme_settings.header_block', 'admin.theme_settings.heading', 'admin.theme_settings.notes', " +
			"'admin.theme_settings.notes_heading', 'admin.theme_settings.save')",
		SQL: mustSQL("192_i18n_seed_admin_site_structure.sql"),
	})

	// 193：后台系统管理类模板文案词条（审计 I18N-001 组F）。
	// 判据按**本批自己的 key** 枚举计数（342 个全列）—— 用总量会被其它批次的行满足而静默跳过；
	// 前缀 LIKE 也不够：shell.login.* 另有 13 条既有词条，用 LIKE 会虚高。
	registerSeed(Seed{
		Version:      "193-i18n-seed-admin-system",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 342 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.admins.action.cancel', 'admin.admins.action.create', 'admin.admins.action.create_submit', 'admin.admins.action.delete', 'admin.admins.action.edit', 'admin.admins.action.filter', 'admin.admins.action.reset', 'admin.admins.action.save', 'admin.admins.col.actions', 'admin.admins.col.email', 'admin.admins.col.name', 'admin.admins.col.phone', 'admin.admins.col.status', 'admin.admins.col.username', 'admin.admins.confirm.delete', 'admin.admins.edit.title', 'admin.admins.empty', 'admin.admins.field.email', 'admin.admins.field.password', 'admin.admins.field.phone', 'admin.admins.field.remark', 'admin.admins.field.username', 'admin.admins.list.heading', 'admin.admins.list.headingClose', 'admin.admins.ph.email', 'admin.admins.ph.name', 'admin.admins.status.banned', 'admin.admins.status.disabled', 'admin.admins.status.enabled', 'admin.admins.status.unknown', 'admin.analytics.badge.paths', 'admin.analytics.badge.views', 'admin.analytics.badge.visitors', 'admin.analytics.col.day', 'admin.analytics.col.path', 'admin.analytics.col.views', 'admin.analytics.col.visitors', 'admin.analytics.daily.empty.lead', 'admin.analytics.daily.empty.strong', 'admin.analytics.daily.empty.tail', 'admin.analytics.daily.title', 'admin.analytics.error', 'admin.analytics.intro.after', 'admin.analytics.intro.lead', 'admin.analytics.metrics.lead', 'admin.analytics.metrics.strong', 'admin.analytics.metrics.tail', 'admin.analytics.no_project.lead', 'admin.analytics.no_project.link', 'admin.analytics.no_project.tail', 'admin.analytics.paths.empty', 'admin.analytics.paths.title', 'admin.analytics.range.actual_mid', 'admin.analytics.range.actual_post', 'admin.analytics.range.actual_pre', 'admin.analytics.range.from', 'admin.analytics.range.last30', 'admin.analytics.range.submit', 'admin.analytics.range.title', 'admin.analytics.range.to', 'admin.analytics.title', 'admin.dashboard.action.cancel', 'admin.dashboard.action.delete', 'admin.dashboard.action.edit', 'admin.dashboard.action.export', 'admin.dashboard.action.new_admin', 'admin.dashboard.action.save', 'admin.dashboard.badge.draft', 'admin.dashboard.badge.pending_review', 'admin.dashboard.badge.published', 'admin.dashboard.badge.rejected', 'admin.dashboard.badges.title', 'admin.dashboard.buttons.small', 'admin.dashboard.buttons.title', 'admin.dashboard.checkbox.welcome_mail', 'admin.dashboard.col.actions', 'admin.dashboard.col.dept', 'admin.dashboard.col.last_login', 'admin.dashboard.col.name', 'admin.dashboard.col.role', 'admin.dashboard.col.status', 'admin.dashboard.col.username', 'admin.dashboard.dept.content', 'admin.dashboard.dept.market', 'admin.dashboard.dept.tech', 'admin.dashboard.field.dept', 'admin.dashboard.field.email', 'admin.dashboard.field.name', 'admin.dashboard.field.remark', 'admin.dashboard.field.role', 'admin.dashboard.field.status', 'admin.dashboard.field.username', 'admin.dashboard.form.required_hint', 'admin.dashboard.health.degraded', 'admin.dashboard.health.down', 'admin.dashboard.health.ok', 'admin.dashboard.health.unknown', 'admin.dashboard.hint.username', 'admin.dashboard.list.title', 'admin.dashboard.option.dept_placeholder', 'admin.dashboard.option.role_placeholder', 'admin.dashboard.pager.next', 'admin.dashboard.pager.prev', 'admin.dashboard.pager.summary', 'admin.dashboard.ph.name', 'admin.dashboard.ph.remark', 'admin.dashboard.ph.username', 'admin.dashboard.role.developer', 'admin.dashboard.role.editor', 'admin.dashboard.role.super_admin', 'admin.dashboard.role.viewer', 'admin.dashboard.row.name_li', 'admin.dashboard.row.name_wang', 'admin.dashboard.row.name_zhang', 'admin.dashboard.row.never_login', 'admin.dashboard.stat.admins', 'admin.dashboard.stat.admins_delta', 'admin.dashboard.stat.online', 'admin.dashboard.stat.online_note', 'admin.dashboard.stat.pending', 'admin.dashboard.stat.pending_note', 'admin.dashboard.stat.published', 'admin.dashboard.stat.published_note', 'admin.dashboard.status.disabled', 'admin.dashboard.status.disabled_row', 'admin.dashboard.status.enabled', 'admin.dashboard.status.pending', 'admin.dashboard.subtitle', 'admin.dashboard.title', 'admin.datarules.action.back', 'admin.datarules.action.cancel', 'admin.datarules.action.create', 'admin.datarules.action.create_short', 'admin.datarules.action.create_submit', 'admin.datarules.action.delete', 'admin.datarules.action.edit', 'admin.datarules.action.edit_config', 'admin.datarules.action.filter', 'admin.datarules.action.reset', 'admin.datarules.action.save', 'admin.datarules.action.save_plain', 'admin.datarules.col.actions', 'admin.datarules.col.domain', 'admin.datarules.col.name', 'admin.datarules.col.remark', 'admin.datarules.col.status', 'admin.datarules.col.updated_at', 'admin.datarules.confirm.delete', 'admin.datarules.edit.title', 'admin.datarules.edit_page_title', 'admin.datarules.empty', 'admin.datarules.field.config', 'admin.datarules.field.config_plain', 'admin.datarules.field.domain', 'admin.datarules.field.name', 'admin.datarules.field.remark', 'admin.datarules.field.rule_config', 'admin.datarules.field.status', 'admin.datarules.hint.config', 'admin.datarules.list.heading', 'admin.datarules.list.headingClose', 'admin.datarules.paren_close', 'admin.datarules.paren_open', 'admin.datarules.ph.domain', 'admin.datarules.status.disabled', 'admin.datarules.status.enabled', 'admin.depts.action.cancel', 'admin.depts.action.create', 'admin.depts.action.create_submit', 'admin.depts.action.delete', 'admin.depts.action.edit', 'admin.depts.action.save', 'admin.depts.col.actions', 'admin.depts.col.code', 'admin.depts.col.name', 'admin.depts.col.remark', 'admin.depts.col.sort', 'admin.depts.col.status', 'admin.depts.confirm.delete', 'admin.depts.edit.title', 'admin.depts.empty', 'admin.depts.field.code', 'admin.depts.field.name', 'admin.depts.field.parent', 'admin.depts.field.remark', 'admin.depts.field.sort', 'admin.depts.field.status', 'admin.depts.list.heading', 'admin.depts.list.headingClose', 'admin.depts.option.root', 'admin.depts.ph.code_example', 'admin.depts.ph.filter', 'admin.depts.ph.name_example', 'admin.depts.status.disabled', 'admin.depts.status.enabled', 'admin.i18n.action.delete', 'admin.i18n.action.edit', 'admin.i18n.action.filter', 'admin.i18n.action.save', 'admin.i18n.form.hint', 'admin.i18n.form.title', 'admin.i18n.intro', 'admin.i18n.label.category', 'admin.i18n.label.lang', 'admin.i18n.label.remark', 'admin.i18n.label.updated_at', 'admin.i18n.label.value', 'admin.i18n.option.all_cats', 'admin.i18n.option.all_langs', 'admin.i18n.pager.page_post', 'admin.i18n.pager.total_post', 'admin.i18n.pager.total_pre', 'admin.i18n.ph.keyword', 'admin.i18n.ph.remark', 'admin.i18n.roles.after1', 'admin.i18n.roles.after2', 'admin.i18n.roles.strong1', 'admin.i18n.roles.strong2', 'admin.i18n.roles.strong3', 'admin.i18n.roles.tail', 'admin.i18n.saved', 'admin.i18n.title', 'admin.i18n.unsaved', 'admin.masterdata.action.filter', 'admin.masterdata.action.reset', 'admin.masterdata.action.view_history', 'admin.masterdata.col.action', 'admin.masterdata.col.actions', 'admin.masterdata.col.entity', 'admin.masterdata.col.field', 'admin.masterdata.col.new', 'admin.masterdata.col.old', 'admin.masterdata.col.operator', 'admin.masterdata.col.origin', 'admin.masterdata.col.time', 'admin.masterdata.col2.changes', 'admin.masterdata.col2.last_action', 'admin.masterdata.col2.last_field', 'admin.masterdata.col2.last_operator', 'admin.masterdata.col2.last_time', 'admin.masterdata.current.empty', 'admin.masterdata.current.entity_id_label', 'admin.masterdata.current.paren_close', 'admin.masterdata.current.paren_open', 'admin.masterdata.current.snapshot', 'admin.masterdata.current.title', 'admin.masterdata.current.total_post', 'admin.masterdata.current.total_pre', 'admin.masterdata.entities.empty', 'admin.masterdata.entities.heading', 'admin.masterdata.entities.headingClose', 'admin.masterdata.entities.hint', 'admin.masterdata.filter.hint', 'admin.masterdata.filter.title', 'admin.masterdata.intro1.after', 'admin.masterdata.intro1.lead', 'admin.masterdata.intro1.strong', 'admin.masterdata.intro2.after1', 'admin.masterdata.intro2.lead', 'admin.masterdata.intro2.strong1', 'admin.masterdata.intro2.strong2', 'admin.masterdata.intro2.tail', 'admin.masterdata.option.action_all', 'admin.masterdata.option.entity_all', 'admin.masterdata.partial_error', 'admin.masterdata.ph.entity_id', 'admin.masterdata.ph.field', 'admin.masterdata.ph.keyword', 'admin.masterdata.ph.operator', 'admin.masterdata.rows.empty', 'admin.masterdata.rows.heading', 'admin.masterdata.rows.headingClose', 'admin.masterdata.title', 'admin.masterdata.truncated.mid', 'admin.masterdata.truncated.post', 'admin.masterdata.truncated.pre', 'admin.permissions.action.cancel', 'admin.permissions.action.create', 'admin.permissions.action.create_submit', 'admin.permissions.action.delete', 'admin.permissions.action.edit', 'admin.permissions.action.filter', 'admin.permissions.action.reset', 'admin.permissions.action.save', 'admin.permissions.col.actions', 'admin.permissions.col.api_path', 'admin.permissions.col.code', 'admin.permissions.col.method', 'admin.permissions.col.module', 'admin.permissions.col.name', 'admin.permissions.col.remark', 'admin.permissions.col.status', 'admin.permissions.confirm.delete', 'admin.permissions.edit.title', 'admin.permissions.empty', 'admin.permissions.field.api_path', 'admin.permissions.field.code', 'admin.permissions.field.method', 'admin.permissions.field.module', 'admin.permissions.field.name', 'admin.permissions.field.remark', 'admin.permissions.field.status', 'admin.permissions.list.heading', 'admin.permissions.list.headingClose', 'admin.permissions.ph.api_path_example', 'admin.permissions.ph.code', 'admin.permissions.ph.code_example', 'admin.permissions.ph.module', 'admin.permissions.ph.module_example', 'admin.permissions.status.disabled', 'admin.permissions.status.enabled', 'admin.roles.action.cancel', 'admin.roles.action.create', 'admin.roles.action.create_submit', 'admin.roles.action.delete', 'admin.roles.action.edit', 'admin.roles.action.filter', 'admin.roles.action.reset', 'admin.roles.action.save', 'admin.roles.badge.system', 'admin.roles.col.actions', 'admin.roles.col.code', 'admin.roles.col.name', 'admin.roles.col.remark', 'admin.roles.col.sort', 'admin.roles.col.status', 'admin.roles.confirm.delete', 'admin.roles.edit.title', 'admin.roles.empty', 'admin.roles.field.code', 'admin.roles.field.name', 'admin.roles.field.remark', 'admin.roles.field.sort', 'admin.roles.field.status', 'admin.roles.list.heading', 'admin.roles.list.headingClose', 'admin.roles.ph.code', 'admin.roles.ph.keyword', 'admin.roles.status.disabled', 'admin.roles.status.enabled', 'shell.login.dev_login', 'shell.login.dev_login_hint')",
		SQL:          mustSQL("193_i18n_seed_admin_system.sql"),
	})

	// 216：webhook 模块文案词条（模块接线后补，见迁移文件头）。
	// enums 的值是 i18n key，文案必须有落处 —— 否则响应层 translate 未命中会原样返回 key。
	// 判据按本批自己的 12 个 key 枚举计数：前缀 LIKE "webhook.%" 会被将来其它批次的
	// webhook.* 词条满足而静默跳过。
	registerSeed(Seed{
		Version:   "216-webhook-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 17 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('webhook.msg.saveSuccess', 'webhook.msg.deleteSuccess', " +
			"'webhook.msg.statusSuccess', 'webhook.msg.retryQueued', 'webhook.err.invalidParam', " +
			"'webhook.err.endpointNotFound', 'webhook.err.eventTypeRequired', 'webhook.err.targetUrlRequired', " +
			"'webhook.err.secretRequired', 'webhook.err.deliveryNotFound', 'webhook.err.deliveryNotFailed', " +
			"'webhook.err.deliveryNotPending', 'webhook.err.urlMalformed', 'webhook.err.urlSchemeUnsupported', " +
			"'webhook.err.urlHostMissing', 'webhook.err.urlUnresolvable', 'webhook.err.urlDenied')",
		SQL: mustSQL("216_webhook_i18n.sql"),
	})
	// 217：SEO 控制台模板文案词条（436d20b 引入该页时漏 key 化，门禁因此从第一天就红着）。
	// 判据按本批自己的 38 个 key 枚举计数：前缀 LIKE "admin.seo.%" 会被将来同前缀的词条满足。
	registerSeed(Seed{
		Version:   "217-admin-seo-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 38 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.seo.eyebrow.visibility', 'admin.seo.title', 'admin.seo.subtitle', 'admin.seo.label.project', 'admin.seo.action.view', 'admin.seo.err.project_list', 'admin.seo.eyebrow.issues', 'admin.seo.audit.title', 'admin.seo.audit.intro', 'admin.seo.audit.run', 'admin.seo.audit.placeholder', 'admin.seo.audit.no_permission', 'admin.seo.paths.eyebrow', 'admin.seo.paths.title', 'admin.seo.paths.error', 'admin.seo.paths.col.path', 'admin.seo.paths.col.views', 'admin.seo.paths.col.visitors', 'admin.seo.paths.empty', 'admin.seo.sources.eyebrow', 'admin.seo.sources.title', 'admin.seo.sources.unavailable', 'admin.seo.site_files.eyebrow', 'admin.seo.site_files.title', 'admin.seo.site_files.unavailable', 'admin.seo.external.eyebrow', 'admin.seo.external.title', 'admin.seo.external.unavailable', 'admin.seo.audit.loading', 'admin.seo.audit.request_failed', 'admin.seo.audit.clean_lead', 'admin.seo.audit.clean_tail', 'admin.seo.audit.issues_lead', 'admin.seo.audit.issues_mid', 'admin.seo.audit.issues_tail', 'admin.seo.audit.col.level', 'admin.seo.audit.col.path', 'admin.seo.audit.col.issue')",
		SQL: mustSQL("217_i18n_seed_admin_seo.sql"),
	})
}
