package searchresults

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// View 站内搜索渲染视图。
type View struct {
	Placeholder      string
	BaseFragmentURL  string
	Notice           string
}

// CompileCSS 导出样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成搜索组件视图（projectId / lang / limit 烘进片段基址 URL）。
func BuildView(p *Props, projectID, lang string) View {
	view := View{Placeholder: effectivePlaceholder(p)}
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
