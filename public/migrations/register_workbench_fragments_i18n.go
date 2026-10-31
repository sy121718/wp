package migrations

func init() {
	registerSeed(Seed{
		Version:   "454-i18n-workbench-fragments",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 118 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.article.import.empty', 'admin.article.import.lossless', 'admin.article.import.lossy', " +
			"'admin.article.import.resultCount', 'workbench.bridge.copy', 'workbench.bridge.cut', " +
			"'workbench.bridge.delete', 'workbench.bridge.editText', 'workbench.bridge.entranceGroup', " +
			"'workbench.bridge.hoverLift', 'workbench.bridge.insert', 'workbench.bridge.moveDown', " +
			"'workbench.bridge.moveUp', 'workbench.bridge.pasteInside', 'workbench.ui.history.empty', " +
			"'workbench.ui.history.restore', 'workbench.ui.seo.dupPrefix', 'workbench.ui.seo.dupSuffix', " +
			"'workbench.ui.seo.jumpHint', 'workbench.ui.seo.profileWeight', 'workbench.ui.seo.totalTip', " +
			"'workbench.ui.seo.unavailable', 'workbench.ui.settings.canonical', 'workbench.ui.settings.canonicalPlaceholder', " +
			"'workbench.ui.settings.focusKeyword', 'workbench.ui.settings.focusKeywordPlaceholder', " +
			"'workbench.ui.settings.followOff', 'workbench.ui.settings.followOn', 'workbench.ui.settings.indexOff', " +
			"'workbench.ui.settings.indexOn', 'workbench.ui.settings.intent', 'workbench.ui.settings.intentCommercial', " +
			"'workbench.ui.settings.intentInformational', 'workbench.ui.settings.intentLocal', " +
			"'workbench.ui.settings.intentTransactional', 'workbench.ui.settings.landmarkNone', " +
			"'workbench.ui.settings.layoutBoxed', 'workbench.ui.settings.layoutFull', 'workbench.ui.settings.layoutMode', " +
			"'workbench.ui.settings.mainLandmark', 'workbench.ui.settings.noThemeHint', 'workbench.ui.settings.ogImage', " +
			"'workbench.ui.settings.ogImagePlaceholder', 'workbench.ui.settings.robotsFollow', " +
			"'workbench.ui.settings.robotsIndex', 'workbench.ui.settings.saveHint', " +
			"'workbench.ui.settings.schemaArticle', 'workbench.ui.settings.schemaAuto', " +
			"'workbench.ui.settings.schemaProduct', 'workbench.ui.settings.schemaType', " +
			"'workbench.ui.settings.schemaWebsite', 'workbench.ui.settings.secondaryKeywords', " +
			"'workbench.ui.settings.secondaryKeywordsPlaceholder', 'workbench.ui.settings.seoDescription', " +
			"'workbench.ui.settings.seoDescriptionPlaceholder', 'workbench.ui.settings.seoTitle', " +
			"'workbench.ui.settings.seoTitlePlaceholder', 'workbench.ui.settings.themeOverride', " +
			"'workbench.ui.settings.themeOverridePlaceholder')",
		SQL: mustSQL("454_i18n_workbench_fragments.sql"),
	})
}
