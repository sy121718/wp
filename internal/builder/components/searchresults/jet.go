package searchresults

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// View 站内搜索渲染视图。
type View struct {
	Placeholder     string
	BaseFragmentURL string
	Notice          string
	// LabelSR / SubmitText 搜索框的可访问标签与按钮文字（审计 I18N-010）。
	LabelSR    string
	SubmitText string
}

// CompileCSS 导出样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成搜索组件视图（projectId / lang / limit 烘进片段基址 URL）。
func BuildView(p *Props, projectID, lang string) View {
	// 文案先落中文兜底，ApplyI18n 按语言覆盖（未接入 i18n 时就是最终值）。
	view := View{Placeholder: effectivePlaceholder(p), LabelSR: fallbackLabel, SubmitText: fallbackSubmit}
	if strings.TrimSpace(projectID) == "" {
		view.Notice = "站内搜索暂不可用（未取到站点工程）"
		return view
	}
	q := url.Values{}
	q.Set("projectId", strings.TrimSpace(projectID))
	q.Set("limit", strconv.Itoa(effectiveLimit(p)))
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	view.BaseFragmentURL = searchResultsPath + "?" + q.Encode()
	return view
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：搜索框与结果区都靠
// hx-get 拉片段（无 HTMX 时表单退化为原生 GET 提交到当前页）。Notice 分支只有一句提示，
// 模板不输出表单，这里也不能登记。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Notice != "" {
		return nil, nil
	}
	return []string{"hx-get", "hx-target", "hx-swap", "hx-include", "hx-vals", "hx-trigger"}, nil
}
