package searchresults

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.searchResults"

const searchResultsPath = "/api/site/search"

const (
	defaultLimit       = 8
	defaultPlaceholder = "搜索站内内容"
)

type Props struct {
	Placeholder string             `json:"placeholder,omitempty" ct:"text,maxlen=40,sec=content,label=占位提示"`
	Limit       int                `json:"limit,omitempty" ct:"int,min=1,max=20,default=8,sec=content,label=每类结果条数"`
	Advanced    core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "站内搜索",
		Hint:            "搜索框 + 结果列表（JSON数据）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"placeholder": "搜索站内内容",
			"limit":       8,
		},
		TypeName:     Type,
		Translatable: []string{"placeholder"},
	},
}

func effectivePlaceholder(p *Props) string {
	if s := strings.TrimSpace(p.Placeholder); s != "" {
		return s
	}
	return defaultPlaceholder
}

func effectiveLimit(p *Props) int {
	if p.Limit <= 0 {
		return defaultLimit
	}
	if p.Limit > 20 {
		return 20
	}
	return p.Limit
}

//go:embed searchresults.css
var searchResultsCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSSTmpl(b, sel, searchResultsCSS, nil); err != nil {
		panic(fmt.Sprintf("searchResults 组件样式解析失败: %v", err))
	}
}

//go:embed search_widget.jet
var searchWidgetTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("search_widget", searchWidgetTemplate)
}
