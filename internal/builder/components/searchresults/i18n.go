package searchresults

// i18n.go — 站内搜索外壳的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.searchResults.<语义>。
const (
	TextKeyLabel  = "site.component.searchResults.label"
	TextKeySubmit = "site.component.searchResults.submit"
)

// 中文兜底（缺词条时的回退值，与抽 key 前的产物逐字一致）。
const (
	fallbackLabel  = "搜索"
	fallbackSubmit = "搜索"
)

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 两个 key 而不是一个：`<span class="sr-only">` 是给读屏器的输入框标签，
// 按钮文字是给所有人看的。中英恰好同形（都是 Search），但别的语言不一定 ——
// 合成一个 key 之后就没法单独改了。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.LabelSR = fallbackLabel
		v.SubmitText = fallbackSubmit
		v.EmptyQuery = "请输入搜索关键词"
		v.NoResults = "没有找到与「%s」相关的内容"
		return
	}
	v.LabelSR = text(TextKeyLabel, fallbackLabel)
	v.SubmitText = text(TextKeySubmit, fallbackSubmit)
	v.EmptyQuery = text("site.fragment.search.empty_query", "请输入搜索关键词")
	v.NoResults = text("site.fragment.search.no_results", "没有找到与「%s」相关的内容")
}
