package searchresults

const (
	TextKeyLabel  = "site.component.searchResults.label"
	TextKeySubmit = "site.component.searchResults.submit"
)

const (
	fallbackLabel  = "搜索"
	fallbackSubmit = "搜索"
)

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
