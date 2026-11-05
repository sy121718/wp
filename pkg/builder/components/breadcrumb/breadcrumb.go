package breadcrumb

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.breadcrumb"

const maxItems = 20

const maxLabelLen = 60

const defaultSeparator = "/"

const defaultGap = "8px"

const (
	defaultColor          = "var(--sky-c-text-muted, #6b7280)"
	defaultHoverColor     = "var(--sky-c-primary, #2563eb)"
	defaultActiveColor    = "var(--sky-c-text, #18181b)"
	defaultSeparatorColor = "var(--sky-c-text-muted, #6b7280)"
)

type Item struct {
	Label string `json:"label,omitempty"`

	URL string `json:"url,omitempty"`
}

type Props struct {
	HomeLabel string `json:"homeLabel,omitempty" ct:"text,maxlen=60,sec=content,label=首页文案"`

	Items []Item `json:"items,omitempty"`

	HideHome bool `json:"hideHome,omitempty" ct:"bool,sec=content,label=隐藏首页项"`

	JSONLD bool `json:"jsonLd,omitempty" ct:"bool,sec=content,label=输出 BreadcrumbList 结构化数据"`

	Separator string `json:"separator,omitempty" ct:"safe,maxlen=8,sec=style,label=分隔符"`

	Color          string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=链接色"`
	HoverColor     string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停色"`
	ActiveColor    string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前项色"`
	SeparatorColor string `json:"separatorColor,omitempty" ct:"color,maxlen=200,sec=style,label=分隔符色"`
	FontSize       string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	Gap            string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "面包屑",
		Hint:            "当前页层级（自动按路径派生）",
		PaletteCategory: core.PaletteCategoryBasic,
		TypeName:        Type,
		ValidateExtra:   validateExtra,

		Translatable: []string{"homeLabel", "label"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if len(p.Items) > maxItems {
		return fmt.Errorf("面包屑层级过多（上限 %d 项）", maxItems)
	}
	for i, it := range p.Items {
		label := strings.TrimSpace(it.Label)
		if label == "" {
			return fmt.Errorf("第 %d 个面包屑项缺少文字", i+1)
		}
		if len([]rune(label)) > maxLabelLen {
			return fmt.Errorf("第 %d 个面包屑项文字过长（上限 %d 字符）", i+1, maxLabelLen)
		}
		if u := strings.TrimSpace(it.URL); u != "" {
			if len(u) > 300 || !core.IsSafeURL(u) {
				return fmt.Errorf("第 %d 个面包屑项链接非法: %q", i+1, u)
			}
		}
	}

	if p.HideHome && len(p.Items) == 0 {
		return fmt.Errorf("隐藏首页项时必须手工提供面包屑层级（否则没有可显示的层级）")
	}
	return nil
}

func effectiveValue(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

//go:embed breadcrumb.css
var breadcrumbCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"color":          effectiveValue(p.Color, defaultColor),
		"hoverColor":     effectiveValue(p.HoverColor, defaultHoverColor),
		"activeColor":    effectiveValue(p.ActiveColor, defaultActiveColor),
		"separatorColor": effectiveValue(p.SeparatorColor, defaultSeparatorColor),
		"fontSize":       p.FontSize,
		"gap":            effectiveValue(p.Gap, defaultGap),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, breadcrumbCSS, vars); err != nil {

		panic(fmt.Sprintf("breadcrumb 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("breadcrumb", breadcrumbTemplate)
}

//go:embed breadcrumb.jet
var breadcrumbTemplate string
