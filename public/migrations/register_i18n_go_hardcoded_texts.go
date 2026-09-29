package migrations

func init() {
	registerSeed(Seed{
		Version:   "451-i18n-go-hardcoded-texts",
		TableName: "sys_i18n",
		// 门槛判据**逐条枚举本批自己的 item_key**（上界封闭）：条目恰好 488 条
		//（244 个 key × zh-CN / en-US）。用 LIKE 前缀或全库总量都不行 ——
		// 前者会被同前缀的其它批次顶高（058 的静默跳过），后者永远追不平（076 的每次重跑）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 488 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.article.err.missingID', 'admin.article.err.missingSlug', 'admin.article.err.missingTitle', 'admin.article.import.action.drop', 'admin.article.import.action.placeholder', 'admin.article.import.action.trim', " +
			"'admin.article.import.action.unwrap', 'admin.article.import.err.depsMissing', 'admin.article.import.err.emptyBody', 'admin.article.import.err.noPageCapability', 'admin.article.import.err.noPath', 'admin.article.import.err.noProject', " +
			"'admin.article.import.hint.noProject', 'admin.article.import.hint.saveFirst', 'admin.article.import.node.container', 'admin.article.import.node.divider', 'admin.article.import.node.heading', 'admin.article.import.node.image', " +
			"'admin.article.import.node.list', 'admin.article.import.node.quote', 'admin.article.import.node.table', 'admin.article.import.node.text', 'admin.article.ok.created', 'admin.article.ok.deleted', " +
			"'admin.article.ok.saved', 'admin.article.publish.err.depsMissing', 'admin.article.publish.err.newPathRequired', 'admin.article.publish.err.noProject', 'admin.article.publish.err.noTemplatePick', 'admin.article.publish.err.noURLPath', " +
			"'admin.article.publish.err.unavailable', 'admin.article.publish.hint.noTemplate', 'admin.article.publish.hint.saveFirst', 'admin.article.publish.ok.published', 'admin.article.publish.ok.rebuilt', 'admin.article.publish.ok.urlUpdated', " +
			"'admin.article.score.serpDescEmpty', 'admin.article.score.serpTitleEmpty', 'admin.article.state.published', 'admin.article.state.unpublished', 'admin.article.translations.err.contextInvalid', 'admin.article.translations.err.countFailed', " +
			"'admin.article.translations.err.depsMissing', 'admin.article.translations.err.listFailed', 'admin.article.translations.err.notTranslatable', 'admin.article.translations.err.pageChanged', 'admin.article.translations.err.richMismatch', 'admin.article.translations.err.rowCountMismatch', " +
			"'admin.article.translations.err.saveFailed', 'admin.article.translations.err.sourceChanged', 'admin.article.translations.err.storageUnavailable', 'admin.article.translations.field.body', 'admin.article.translations.field.excerpt', 'admin.article.translations.field.seoDescription', " +
			"'admin.article.translations.field.seoTitle', 'admin.article.translations.field.title', 'admin.article.translations.saved', 'admin.article.translations.savedNone', 'admin.blocks.bulkResult.allDeleted', 'admin.blocks.bulkResult.allSkipped', " +
			"'admin.blocks.bulkResult.noneSelected', 'admin.blocks.bulkResult.partial', 'admin.blocks.impact.unavailable.noPageCapability', 'admin.blocks.impact.unavailable.projectReadFailed', 'admin.blocks.refCount.none', 'admin.blocks.refCount.pages', " +
			"'admin.blocks.refCount.unknown', 'admin.blocks.reuseMode.global', 'admin.blocks.reuseMode.template', 'admin.blocks.usage.more', 'admin.media.package.dir', 'admin.media.package.originalMissing', " +
			"'admin.media.package.originalPathInvalid', 'admin.media.package.readFailed', 'admin.media.package.regenHint', 'admin.media.package.status', 'admin.media.package.title', 'admin.media.package.variantType', " +
			"'admin.media.package.variantUnavailable', 'admin.media.variantStatus.failed', 'admin.media.variantStatus.missing', 'admin.media.variantStatus.pending', 'admin.media.variantStatus.processing', 'admin.navigation_translations.err.contextInvalid', " +
			"'admin.navigation_translations.err.notTranslatable', 'admin.navigation_translations.err.projectListFailed', 'admin.navigation_translations.err.rowCountMismatch', 'admin.navigation_translations.err.saveFailed', 'admin.navigation_translations.err.sourceChanged', 'admin.navigation_translations.err.storageUnavailable', " +
			"'admin.navigation_translations.saved', 'admin.navigation_translations.savedNone', 'admin.navigations.bulkResult.allDeleted', 'admin.navigations.bulkResult.allSkipped', 'admin.navigations.bulkResult.noneSelected', 'admin.navigations.bulkResult.partial', " +
			"'admin.navigations.notice.noSourcePicked', 'admin.navigations.panel.blockName.footer', 'admin.navigations.panel.blockName.footerMobile', 'admin.navigations.panel.blockName.header', 'admin.navigations.panel.blockName.headerMobile', 'admin.navigations.panel.blockName.menu', " +
			"'admin.navigations.panel.blockName.panel', 'admin.navigations.source.article', 'admin.navigations.source.category', 'admin.navigations.source.page', 'admin.navigations.source.product', 'admin.page_translations.origin.block', " +
			"'admin.page_translations.origin.footer', 'admin.page_translations.origin.header', 'admin.page_translations.reuse.moreSuffix', 'admin.page_translations.reuse.pathSeparator', 'admin.pages.history.loadFailed', 'admin.seo.score.grade.excellent', " +
			"'admin.seo.score.grade.good', 'admin.seo.score.grade.missing', 'admin.seo.score.grade.needsWork', 'admin.seo.score.grade.poor', 'admin.seo.score.issueFormat', 'admin.seo.score.serpDescEmpty', " +
			"'admin.seo.score.serpTitleEmpty', 'admin.site_slots.action.bind', 'admin.site_slots.action.rebind', 'admin.site_slots.err.noPage', 'admin.site_slots.err.noProject', 'admin.site_slots.ok.bound', " +
			"'admin.site_slots.ok.unbound', 'admin.site_slots.slot.account.name', 'admin.site_slots.slot.account.usage', 'admin.site_slots.slot.blog.name', 'admin.site_slots.slot.blog.usage', 'admin.site_slots.slot.cart.name', " +
			"'admin.site_slots.slot.cart.usage', 'admin.site_slots.slot.checkout.name', 'admin.site_slots.slot.checkout.usage', 'admin.site_slots.slot.forgot.name', 'admin.site_slots.slot.forgot.usage', 'admin.site_slots.slot.login.name', " +
			"'admin.site_slots.slot.login.usage', 'admin.site_slots.slot.orders.name', 'admin.site_slots.slot.orders.usage', 'admin.site_slots.slot.register.name', 'admin.site_slots.slot.register.usage', 'admin.site_slots.slot.reset.name', " +
			"'admin.site_slots.slot.reset.usage', 'admin.site_slots.slot.shop.name', 'admin.site_slots.slot.shop.usage', 'admin.site_slots.state.deleted', 'admin.site_slots.state.published', 'admin.site_slots.state.unbound', " +
			"'admin.site_slots.state.unpublished', 'admin.theme_settings.block.unset', 'admin.theme_settings.err.fieldInvalid', 'admin.theme_settings.label.accentColor', 'admin.theme_settings.label.bodyColor', 'admin.theme_settings.label.bodySize', " +
			"'admin.theme_settings.label.borderColor', 'admin.theme_settings.label.borderStyle', 'admin.theme_settings.label.borderWidth', 'admin.theme_settings.label.button', 'admin.theme_settings.label.buttonBackground', 'admin.theme_settings.label.buttonTextColor', " +
			"'admin.theme_settings.label.cardSurface', 'admin.theme_settings.label.colors', 'admin.theme_settings.label.dangerColor', 'admin.theme_settings.label.defaultShadow', 'admin.theme_settings.label.easing', 'admin.theme_settings.label.entrance', " +
			"'admin.theme_settings.label.fontFamily', 'admin.theme_settings.label.fontWeight', 'admin.theme_settings.label.globalRadius', 'admin.theme_settings.label.headingColor', 'admin.theme_settings.label.headingSize', 'admin.theme_settings.label.headingSpacing', " +
			"'admin.theme_settings.label.hoverBackground', 'admin.theme_settings.label.hoverTextColor', 'admin.theme_settings.label.images', 'admin.theme_settings.label.lazyDefault', 'admin.theme_settings.label.lazySkeleton', 'admin.theme_settings.label.lineHeight', " +
			"'admin.theme_settings.label.linkHoverColor', 'admin.theme_settings.label.motion', 'admin.theme_settings.label.optBorderDashed', 'admin.theme_settings.label.optBorderDotted', 'admin.theme_settings.label.optBorderDouble', 'admin.theme_settings.label.optBorderNone', " +
			"'admin.theme_settings.label.optBorderSolid', 'admin.theme_settings.label.optDefault', 'admin.theme_settings.label.optDuration200Recommended', 'admin.theme_settings.label.optDurationNone', 'admin.theme_settings.label.optFadeIn', 'admin.theme_settings.label.optFadeUp', " +
			"'admin.theme_settings.label.optFontChineseSans', 'admin.theme_settings.label.optFontGeorgia', 'admin.theme_settings.label.optFontInter', 'admin.theme_settings.label.optFontMono', 'admin.theme_settings.label.optFontSerif', 'admin.theme_settings.label.optFontSystemRecommended', " +
			"'admin.theme_settings.label.optFontThemeDefault', 'admin.theme_settings.label.optLarge', 'admin.theme_settings.label.optLineHeight14Tight', 'admin.theme_settings.label.optLineHeight16Common', 'admin.theme_settings.label.optLineHeight17Recommended', 'admin.theme_settings.label.optLineHeight18Loose', " +
			"'admin.theme_settings.label.optMedium', 'admin.theme_settings.label.optNone', 'admin.theme_settings.label.optOff', 'admin.theme_settings.label.optOn', 'admin.theme_settings.label.optOnRecommended', 'admin.theme_settings.label.optRadius10Recommended', " +
			"'admin.theme_settings.label.optRadius12Recommended', 'admin.theme_settings.label.optRadius8Recommended', 'admin.theme_settings.label.optRadiusNone', 'admin.theme_settings.label.optRadiusPill', 'admin.theme_settings.label.optSize16Recommended', 'admin.theme_settings.label.optSize20Recommended', " +
			"'admin.theme_settings.label.optSize32Recommended', 'admin.theme_settings.label.optSlideUp', 'admin.theme_settings.label.optSmall', 'admin.theme_settings.label.optUnderlineAlways', 'admin.theme_settings.label.optUnderlineHover', 'admin.theme_settings.label.optWeightBold', " +
			"'admin.theme_settings.label.optWeightMedium', 'admin.theme_settings.label.optWeightRegular', 'admin.theme_settings.label.optWeightSemiBold', 'admin.theme_settings.label.optXLarge', 'admin.theme_settings.label.optZoomIn', 'admin.theme_settings.label.paddingX', " +
			"'admin.theme_settings.label.paddingY', 'admin.theme_settings.label.pageBackground', 'admin.theme_settings.label.primaryColor', 'admin.theme_settings.label.radius', 'admin.theme_settings.label.secondaryColor', 'admin.theme_settings.label.shadow', " +
			"'admin.theme_settings.label.successColor', 'admin.theme_settings.label.surface', 'admin.theme_settings.label.transitionDuration', 'admin.theme_settings.label.typographyBody', 'admin.theme_settings.label.typographyHeading', 'admin.theme_settings.label.typographyLink', " +
			"'admin.theme_settings.label.underline', 'admin.theme_settings.label.warningColor', 'admin.theme_settings.ok.saved', 'admin.theme_settings.structure.current' " +
			") AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("451_i18n_go_hardcoded_texts.sql"),
	})
}
