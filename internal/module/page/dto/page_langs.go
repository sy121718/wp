package pagedto

// page_langs.go — 页面级语言排除的跨模块形状（迁移 491）。
//
// 放 dto（而不是 service）：后台面板经 contract 取它，契约只能依赖 dto 与不可变形状。

// PageLangState 该页某个启用语言的当前状态（默认语言在前）。
type PageLangState struct {
	// Lang 完整语言码。
	Lang string `json:"lang"`
	// IsDefault 是否站点默认语言（默认语言不可排除）。
	IsDefault bool `json:"isDefault"`
	// Excluded 本页是否已排除该语言（该语言本页不产出）。
	Excluded bool `json:"excluded"`
	// Published 该语言当前是否有已激活产物（访问面事实来自 page_publications）。
	Published bool `json:"published"`
}
