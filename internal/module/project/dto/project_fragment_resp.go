package projectdto

// project_fragment_resp.go — 项目模块**片段出口**的渲染视图（htmx 局部替换用）。
//
// 与整页视图分开：片段的键集通常只有一个槽位（Settings / Score / Locales …），
// 整页的键集是一屏的数据。混在一个文件里会让「这一份是整页还是片段」看不出来，
// 而两者的失败形态不同（整页失败要给页壳，片段失败只给一块）。
//
// **导出字段名就是模板契约**：internal/templates/fragments/*.html 下的 `.Field`。

// SettingsPanelView 站点设置面板片段（fragments/settings_panel）。
type SettingsPanelView struct {
	Settings any
}

// SeoScorePanelView SEO 评分区片段（fragments/seo_score）。
type SeoScorePanelView struct {
	Score any
}

// LocaleRowsView 多语言行片段（admin/partials/locale_rows）。
type LocaleRowsView struct {
	Locales any
}

// GlobalPanelView 全局设置面板片段（fragments/global_panel）。
type GlobalPanelView struct {
	ThemeID string
	Groups  any
}
