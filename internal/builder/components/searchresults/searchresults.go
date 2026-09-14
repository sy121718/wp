// Package searchresults 实现 core.searchResults 站内搜索组件（BIZ-2 / BIZ-6）。
//
// 产物只输出搜索框 + 结果挂载点；命中列表由 /_fragments/searchResults 现拉。
// 构建期把 projectId / lang / limit 烘进片段 URL —— 与 cartIcon / orderList 同口径，
// 多语言站点必须带 lang，否则 locator 只能回落默认语言路径。
package searchresults

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.searchResults"

const searchResultsPath = "/_fragments/searchResults"

const (
	defaultLimit       = 8
	defaultPlaceholder = "搜索站内内容"
)

// Props 站内搜索属性。
type Props struct {
	Placeholder string `json:"placeholder,omitempty" ct:"text,maxlen=40,sec=content,label=占位提示"`
	Limit       int    `json:"limit,omitempty" ct:"int,min=1,max=20,default=8,sec=content,label=每类结果条数"`
	Advanced    core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
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
