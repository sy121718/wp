package pagedto

// PageLangStateResp 该页某个启用语言的当前状态（默认语言在前）。
type PageLangStateResp struct {
	// Lang 完整语言码。
	Lang string `json:"lang"`
	// IsDefault 是否站点默认语言（默认语言不可排除）。
	IsDefault bool `json:"isDefault"`
	// Excluded 本页是否已排除该语言（该语言本页不产出）。
	Excluded bool `json:"excluded"`
	// Published 该语言当前是否有已激活产物（访问面事实来自 page_publications）。
	Published bool `json:"published"`
}

// TranslationMissPageResp 未翻译清单页（/admin/page-translation-misses）的视图。
//
// 它是**报告片段**：整页由前端壳承载，这个 handler 只出报告区那块。
type TranslationMissPageResp struct {
	CSRFToken       string
	Projects        any
	SelectedProject string
	Rows            any
	// Err 取数失败的归口事实（key + 中文兜底），取词在模板里发生。
	Err any
}

// PageHistoryListResp 版本历史列表片段（fragments/history_list）。
type PageHistoryListResp struct {
	// Revisions 用空切片而非 nil：Jet 的 len() 不接受 nil（会渲染失败）。
	Revisions []HistoryRowResp
	// Error 是**事实**（key + 中文兜底），取词在片段模板里做。
	Error any
}

// TranslationErrorsResp 译文保存失败时的**逐行错误片段**（admin/partials/translation_errors.html）。
//
// 片段出口：保存失败就地替换 #page-translations-errors，不重渲整页 ——
// 页面与已填译文都没动过，重渲只会把一次校验失败变成一次全量查询。
type TranslationErrorsResp struct {
	// Errors 逐行错误：Label 是「哪一行」（原文 context 名，可空 = 页面级），
	// Msg 是词条事实 —— 两者在模板里合成一句。
	Errors []TranslationRowErrorResp
}
