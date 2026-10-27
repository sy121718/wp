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

// PageFragmentResp 页面侧栏片段（语言状态 / 定时发布两条）的**共用**视图。
//
// 两份片段的键集是同一个骨架：身份（CSRFToken / PageID / Path）+ 行 + 结论（Err / Done）。
// 合成一个类型的理由是「片段也是渲染出口」：它们走 adminui.PrepareBare，注入的壳键
// 只影响取值函数，行的形状由各自的 Row 类型决定。
//
// 不合并的理由也在此：共用骨架不等于共用语义，所以**各页的行类型分开**，
// 这个结构体只承载骨架字段。
type PageFragmentResp struct {
	CSRFToken string
	PageID    string
	Path      string
	Items     any
	// Err / Done 是**事实**（key + 兜底 + 参数）或归口文案：取词在模板里发生。
	Err  any
	Done any
	// DefaultAt 只有定时发布片段用：默认到点时刻（站点时区下的「一小时后」），
	// 省掉每次手填 —— 运营多数排的是近期动作。语言片段为零值。
	DefaultAt string
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
