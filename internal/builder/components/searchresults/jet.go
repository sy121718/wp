package searchresults

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

type View struct {
	Placeholder     string
	BaseFragmentURL string
	EmptyQuery      string
	NoResults       string
	Notice          string

	LabelSR    string
	SubmitText string
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

func BuildView(p *Props, projectID, lang string) View {

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

func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Notice != "" {
		return nil, nil
	}
	return []string{"data-site-read", "data-site-view", "data-site-search"}, nil
}
