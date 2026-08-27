// Package heading 实现 core.heading 标题组件（规范 docs/02-C1、02-C5）。
// 基座 core.Atom 吸收公共样板（ID/叶子/解码/声明式校验/Advanced/class 织入）。
// 本文件为业务本体：文本或 CMS 绑定、语义标签、排版（共享组 TextStyle）、
// 字重/字间距/转换/装饰/颜色/截断/阴影与对应样式编译。
package heading

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.heading"

// 常量上限。
const maxTextLen = 500

// DecorProps 文本装饰。
type DecorProps struct {
	// Decoration 文本装饰：none/underline/line-through。
	Decoration string `json:"decoration,omitempty" ct:"select,none,underline,line-through"`
	// DecorationColor 装饰线颜色微调。
	DecorationColor string `json:"decorationColor,omitempty" ct:"safe,maxlen=100"`
}

// Binding CMS 字段绑定（规范 §2 Dynamic Binding）。
type Binding struct {
	// Field 字段路径：post.title / product.name / category.name 等。
	Field string `json:"field,omitempty"`
	// Fallback 绑定字段为空时的兜底文本。
	Fallback string `json:"fallback,omitempty" ct:"text,maxlen=500"`
}

// Props core.heading 特有属性 + 共享排版组 + Advanced 通用层。
type Props struct {
	// Text 静态文本（与 Binding 二选一，两者都空报错）。
	Text string `json:"text,omitempty" ct:"text,maxlen=500,sec=content"`
	// Binding CMS 字段绑定（优先于 Text；发布期静态填入）。
	Binding *Binding `json:"binding,omitempty"`
	// Tag 语义标签：h1~h6 / div / span（默认 h2）。
	Tag string `json:"tag,omitempty" ct:"select,h1,h2,h3,h4,h5,h6,div,span,default=h2,sec=content"`
	// Typography 字体排版（三端独立，core.TextStyle 共享组）。
	Typography core.TextStyle `json:"typography,omitempty"`
	// Weight 字重：100~900 或 token（regular/medium/semibold/bold）。
	Weight string `json:"weight,omitempty" ct:"string,maxlen=10,sec=style"`
	// LetterSpacing 字间距。
	LetterSpacing string `json:"letterSpacing,omitempty" ct:"safe,maxlen=20,sec=style"`
	// Transform 文字转换：none/uppercase/lowercase/capitalize。
	Transform string `json:"transform,omitempty" ct:"select,none,uppercase,lowercase,capitalize,sec=style"`
	// Decor 文本装饰。
	Decor DecorProps `json:"decor,omitempty"`
	// Color 文字颜色：色值或主题 Token（var(--color-primary)）。
	Color string `json:"color,omitempty" ct:"safe,maxlen=200,sec=style"`
	// LineClamp 多行截断行数 1~6；0 表示不截断。
	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=6,step=1,sec=style"`
	// TextShadow 文字阴影预设：subtle/strong；空为无。
	TextShadow string `json:"textShadow,omitempty" ct:"select,subtle,strong,sec=style"`
	// Advanced 通用高级属性（规范 docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:     Type,
		ValidateExtra: validateExtra,
		Render:       render,
	},
}

// 字重 Token 与文字阴影预设。
var (
	weightTokenMap = map[string]int{"regular": 400, "medium": 500, "semibold": 600, "bold": 700}
	textShadowPresets = map[string]string{
		"subtle": "0 1px 2px rgba(0,0,0,0.45)",
		"strong": "0 2px 6px rgba(0,0,0,0.6)",
	}
	// decorMap 装饰白名单（ct select 外的手写映射作用于渲染取值）。
	decorationMap = map[string]bool{"none": true, "underline": true, "line-through": true}
	// fieldPathRe 绑定字段路径白名单。
	fieldPathRe = regexpCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
)

// regexpCompile 包内正则编译。
func regexpCompile(pat string) *regexp.Regexp { return regexp.MustCompile(pat) }

// resolveWeight 解析字重：token 或 100~900 数值。
func resolveWeight(w string) (value string, err error) {
	if w == "" {
		return "", nil
	}
	if v, ok := weightTokenMap[strings.ToLower(w)]; ok {
		return strconv.Itoa(v), nil
	}
	n, e := strconv.Atoi(w)
	if e != nil || n < 100 || n > 900 || n%100 != 0 {
		return "", fmt.Errorf("无效的字重: %q", w)
	}
	return w, nil
}

// validateExtra 关系性校验：文本/绑定二选一、绑定路径、装饰、字重、排版组。
func validateExtra(p *Props, nodeID string) (err error) {
	hasText := p.Text != ""
	hasBinding := p.Binding != nil && p.Binding.Field != ""
	if !hasText && !hasBinding {
		return fmt.Errorf("必须提供静态文本或 CMS 绑定")
	}
	if p.Binding != nil && p.Binding.Field != "" {
		if !fieldPathRe.MatchString(p.Binding.Field) {
			return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
		}
	}
	if p.Decor.Decoration != "" && !decorationMap[p.Decor.Decoration] {
		return fmt.Errorf("无效的文本装饰: %q", p.Decor.Decoration)
	}
	if _, err = resolveWeight(p.Weight); err != nil {
		return err
	}
	return core.ValidateTextStyle(nodeID, &p.Typography)
}

// render 内容解析（绑定/Fallback/静态文本）+ 语义标签 + 样式编译。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	if p.Tag == "" {
		p.Tag = "h2"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态文本。
	text := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if h.Content == nil {
			return "", fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := h.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			text = v
		} else if p.Binding.Fallback != "" {
			text = p.Binding.Fallback
		}
	}

	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(p.Tag)
	sb.WriteString(" class=\"")
	sb.WriteString(h.Classes)
	sb.WriteString("\"")
	if h.CustomID != "" {
		sb.WriteString(" id=\"")
		sb.WriteString(h.CustomID)
		sb.WriteString("\"")
	}
	sb.WriteString(">")
	sb.WriteString(html.EscapeString(text))
	sb.WriteString("</")
	sb.WriteString(p.Tag)
	sb.WriteString(">")

	compileCSS(node.ID, p, h.CSS)
	return sb.String(), nil
}

// compileCSS 标题样式：排版组三端声明 + 字重/间距/转换/装饰/颜色/截断/阴影。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string
	desktop = append(desktop, p.Typography.BreakpointDecls(core.BreakpointDesktop)...)
	tablet = append(tablet, p.Typography.BreakpointDecls(core.BreakpointTablet)...)
	mobile = append(mobile, p.Typography.BreakpointDecls(core.BreakpointMobile)...)

	if w, err := resolveWeight(p.Weight); err == nil && w != "" {
		desktop = append(desktop, "font-weight: "+w)
	}
	if p.LetterSpacing != "" {
		desktop = append(desktop, "letter-spacing: "+p.LetterSpacing)
	}
	if p.Transform != "" && p.Transform != "none" {
		desktop = append(desktop, "text-transform: "+p.Transform)
	}
	if p.Decor.Decoration != "" && p.Decor.Decoration != "none" {
		decl := "text-decoration: " + p.Decor.Decoration
		if p.Decor.DecorationColor != "" {
			decl += " " + p.Decor.DecorationColor
		}
		desktop = append(desktop, decl)
	}
	if p.Color != "" {
		desktop = append(desktop, "color: "+p.Color)
	}
	if v, ok := textShadowPresets[p.TextShadow]; p.TextShadow != "" && ok {
		desktop = append(desktop, "text-shadow: "+v)
	}

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointTablet, sel, tablet)
	b.Add(core.BreakpointMobile, sel, mobile)

	// 多行截断：-webkit-box 标准组合。
	if p.LineClamp > 0 {
		b.Add(core.BreakpointDesktop, sel, []string{
			"display: -webkit-box",
			fmt.Sprintf("-webkit-line-clamp: %d", p.LineClamp),
			"-webkit-box-orient: vertical",
			"overflow: hidden",
		})
	}
}

// init 注册标题组件。
func init() {
	core.Register(Widget)
}