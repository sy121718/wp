package migrations

// 454 — 工作台 HTMX 片段与画布桥接脚本的文案词条（i18n 收口第二批）。
//
// 门槛判据（migrator 语义：ConditionSQL 返回 > 0 则跳过本 seed）：
// 数「**本批自己的 key** 在 sys_i18n 里的 (key, lang) 行数」是否恰好 118（59 key × 2 语言）。
//
// 为什么逐条枚举而不是 LIKE 前缀：这是本仓的既有明文约定（见 AGENTS.md §「数据库」与 452 的
// 注释）—— 前缀下已有别的批次的行会让计数虚高、本批被**静默跳过**（058 踩过）；前缀下将来
// 新增同前缀的词条会让计数恒大于门槛、**每次启动重跑**（076 同源）。封闭 IN 列表两个方向都干净：
//
//	· 本批少一条 → 116 → 重跑（不冲突则插回）；
//	· 本批已 seed → 118 → 跳过。
//
// 注意 `workbench.ui.settings.noTheme` **不在本批**（它由迁移 452 seeded，本批的
// global_panel.html 与 workbench/layout.html 复用同一行词条）—— 把它算进来会让计数门槛
// 永远差 2，本批每次启动重跑。这正是上一条「上界必须封闭」的反面教材。
//
// 这 59 个 key 与 454_i18n_workbench_fragments.sql 的 VALUES 行数（118）一一对应：
// 增删词条时两处必须同批改，否则门槛失准 —— 失准的方向是「重跑」而不是「静默跳过」，
// 这是刻意选的失败方向。
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
