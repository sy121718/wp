// Package breadcrumb 实现 core.breadcrumb 可见面包屑组件（审计 SEO-014）。
//
// 此前面包屑只存在于产物 <head> 的 BreadcrumbList JSON-LD 里（由页面 URL 路径推导，
// 见 internal/builder/seo_head.go 的 breadcrumbList），访客在页面上看不到自己在站点里的
// 位置。本组件补齐可见层级链路，并与既有 JSON-LD 共用同一份层级数据来源：
//
//   - 作者未填 items：层级按构建期页面访问路径（RenderContext.CurrentPath）逐段展开，
//     规则与 seo_head.breadcrumbList 完全一致（首页 + 各路径段，名称取解码后的段名），
//     首页名同取 site.breadcrumb.home 词条 —— 即与 head 的 JSON-LD 面包屑同源，
//     不会出现「可见面包屑说一套、结构化数据说另一套」；
//   - 作者填了 items：用作者给的层级（自定义名称/顺序/链接），此时可开 jsonLd，
//     由本组件输出与可见项逐项同源的 BreadcrumbList（同一份 items 序列化）。
//
// 默认不输出 JSON-LD：head 已输出一份，同一页出现两条 BreadcrumbList 会让结构化数据
// 互相打架（尤其作者自定义名称时两者的 name 必然不同），需要组件自带时显式打开。
//
// 无子节点、零客户端 JS：静态构建期一次性算好层级，产物是纯 HTML + CSS。
package breadcrumb

import (
	_ "embed" // breadcrumb.css / breadcrumb.jet 经 go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.breadcrumb"

// maxItems 层级项上限：过深的面包屑已无可读性，也防止超长文档拖慢构建。
const maxItems = 20

// maxLabelLen 单项展示名长度上限（可见字符）。
const maxLabelLen = 60

// defaultSeparator 缺省分隔符（作者未填时）。
const defaultSeparator = "/"

// defaultGap 缺省项间距（作者未填时）：分隔符与相邻项之间留一口气，
// 否则「首页 / 分类 / 详情」会挤成一串连体文本。
const defaultGap = "8px"

// 样式缺省值：走主题 Token + 兜底（与 badge/nav 同一取色约定）。
//
// 面包屑是辅助导航，缺省弱化链接色、当前项用正文色强调。
const (
	defaultColor          = "var(--sky-c-text-muted, #6b7280)"
	defaultHoverColor     = "var(--sky-c-primary, #2563eb)"
	defaultActiveColor    = "var(--sky-c-text, #18181b)"
	defaultSeparatorColor = "var(--sky-c-text-muted, #6b7280)"
)

// Item 面包屑层级项（作者手填时的形状）。
type Item struct {
	// Label 展示名（首页项留空时按当前语言取 site.breadcrumb.home 词条）。
	Label string `json:"label,omitempty"`
	// URL 链接（空 = 纯文本；末项由组件标记为当前项）。
	URL string `json:"url,omitempty"`
}

// Props 面包屑属性。
type Props struct {
	// HomeLabel 首页项文案（空 = 取 site.breadcrumb.home 词条，与 head JSON-LD 同一条）。
	HomeLabel string `json:"homeLabel,omitempty" ct:"text,maxlen=60,sec=content,label=首页文案"`
	// Items 手工层级（空 = 按当前页面路径自动派生；自动模式下首页项由组件补）。
	Items []Item `json:"items,omitempty"`
	// HideHome 隐藏首页项（默认显示，与 head JSON-LD 首项一致）。
	HideHome bool `json:"hideHome,omitempty" ct:"bool,sec=content,label=隐藏首页项"`
	// JSONLD 输出与可见项同源的 BreadcrumbList 结构化数据（见包注释：默认关闭）。
	JSONLD bool `json:"jsonLd,omitempty" ct:"bool,sec=content,label=输出 BreadcrumbList 结构化数据"`
	// Separator 项间分隔符。
	Separator string `json:"separator,omitempty" ct:"safe,maxlen=8,sec=style,label=分隔符"`

	// --- 样式 ---
	Color          string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=链接色"`
	HoverColor     string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停色"`
	ActiveColor    string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前项色"`
	SeparatorColor string `json:"separatorColor,omitempty" ct:"color,maxlen=200,sec=style,label=分隔符色"`
	FontSize       string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	Gap            string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "面包屑",
		Hint:            "当前页层级（自动按路径派生）",
		PaletteCategory: core.PaletteCategoryBasic,
		TypeName:        Type,
		ValidateExtra:   validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// homeLabel 覆盖派生模式下的首页项，label 覆盖手填层级的各项。
		Translatable: []string{"homeLabel", "label"},
	},
}

// validateExtra 关系性校验：层级项文字与链接、隐藏首页时的可显示性。
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
	// 隐藏首页项且没有手填层级 = 没有首项可显示（自动派生必须有首页起头）。
	if p.HideHome && len(p.Items) == 0 {
		return fmt.Errorf("隐藏首页项时必须手工提供面包屑层级（否则没有可显示的层级）")
	}
	return nil
}

// effectiveValue 有效取值（空则用兜底值）。
func effectiveValue(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// breadcrumbCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed breadcrumb.css
var breadcrumbCSS string

// compileCSS 面包屑样式：可选属性走「变量为空即整条省略」，颜色缺省跟主题 Token。
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
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("breadcrumb 组件样式解析失败: %v", err))
	}
}

// init 注册面包屑组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("breadcrumb", breadcrumbTemplate)
}

// breadcrumbTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed breadcrumb.jet
var breadcrumbTemplate string
