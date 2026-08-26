// Package heading 实现 core.heading 标题组件（规范 docs/02-C1）：
// 全站主副标题、区块大字、卡片标题等短文本场景。
// 编译期直出单层语义 HTML 标签（h1~h6/div/span），样式全部编译为纯净 CSS，
// 零客户端 JS；CMS 动态绑定在发布期静态填入（含 Fallback 兜底）。
// 基础盒模型与通用样式继承自 02-C0 Advanced 层。
package heading

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.heading"

var (
	// allowedTags 语义标签白名单：h1~h6 + div/span（纯视觉大字，不干扰 SEO 结构）。
	allowedTags = map[string]bool{
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"div": true, "span": true,
	}
	// transformMap 文字转换关键字到 CSS 值。
	transformMap = map[string]string{
		"none": "none", "uppercase": "uppercase", "lowercase": "lowercase", "capitalize": "capitalize",
	}
	// alignMap 文字对齐关键字。
	alignMap = map[string]bool{"left": true, "center": true, "right": true, "justify": true}
	// decorationMap 文本装饰关键字。
	decorationMap = map[string]bool{"none": true, "underline": true, "line-through": true}
	// weightTokenMap 主题字重 Token 到数值。
	weightTokenMap = map[string]int{"regular": 400, "medium": 500, "semibold": 600, "bold": 700}
	// textShadowPresets 轻量文字投影预设（复杂背景上的可读性）。
	textShadowPresets = map[string]string{
		"subtle": "0 1px 2px rgba(0,0,0,0.45)",
		"strong": "0 2px 6px rgba(0,0,0,0.6)",
	}
	// fieldPathRe 绑定字段路径白名单：实体.字段 形式（如 post.title）。
	fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	// clampRe clamp() 流式字号白名单：clamp(数字单位, 数字单位, 数字单位)。
	clampRe = regexp.MustCompile(`^clamp\([0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw)\)$`)
	// lenValueRe 普通长度值（字号/行高/字间距）：可带单位或纯数字（行高）。
	lenValueRe = regexp.MustCompile(`^[0-9.]+(px|rem|em|vw|%)?$`)
)

// maxTextLen 文本长度上限。
const maxTextLen = 500

// ResponsiveTypography 三端排版值。
type ResponsiveTypography struct {
	Desktop Typography `json:"desktop,omitempty"`
	Tablet  Typography `json:"tablet,omitempty"`
	Mobile  Typography `json:"mobile,omitempty"`
}

// Typography 单端排版参数。
type Typography struct {
	// FontSize 字号：px/rem/em/vw 或 clamp() 流式字号。
	FontSize string `json:"fontSize,omitempty"`
	// LineHeight 行高：倍数（如 1.2）或长度值。
	LineHeight string `json:"lineHeight,omitempty"`
	// TextAlign 文字对齐（三端独立）：left/center/right/justify。
	TextAlign string `json:"textAlign,omitempty"`
}

// DecorProps 文本装饰。
type DecorProps struct {
	// Decoration 文本装饰：none/underline/line-through。
	Decoration string `json:"decoration,omitempty"`
	// DecorationColor 装饰线颜色微调。
	DecorationColor string `json:"decorationColor,omitempty"`
}

// Binding CMS 字段绑定（规范 §2 Dynamic Binding）。
type Binding struct {
	// Field 字段路径：post.title / product.name / category.name 等。
	Field string `json:"field,omitempty"`
	// Fallback 绑定字段为空时的兜底文本。
	Fallback string `json:"fallback,omitempty"`
}

// Props core.heading 特有属性 + Advanced 通用层。
type Props struct {
	// Text 静态文本（与 Binding 二选一，两者都空报错）。
	Text string `json:"text,omitempty"`
	// Binding CMS 字段绑定（优先于 Text；发布期静态填入）。
	Binding *Binding `json:"binding,omitempty"`
	// Tag 语义标签：h1~h6 / div / span（默认 h2）。
	Tag string `json:"tag,omitempty"`
	// Typography 字体排版（三端独立）。
	Typography ResponsiveTypography `json:"typography,omitempty"`
	// Weight 字重：100~900 或 token（regular/medium/semibold/bold）。
	Weight string `json:"weight,omitempty"`
	// LetterSpacing 字间距。
	LetterSpacing string `json:"letterSpacing,omitempty"`
	// Transform 文字转换：none/uppercase/lowercase/capitalize。
	Transform string `json:"transform,omitempty"`
	// Decor 文本装饰。
	Decor DecorProps `json:"decor,omitempty"`
	// Color 文字颜色：色值或主题 Token（var(--color-primary)）。
	Color string `json:"color,omitempty"`
	// LineClamp 多行截断行数 1~6；0 表示不截断。
	LineClamp int `json:"lineClamp,omitempty"`
	// TextShadow 文字阴影预设：subtle/strong；空为无。
	TextShadow string `json:"textShadow,omitempty"`
	// Advanced 通用高级属性（规范 docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Heading core.heading 组件实现。
type Heading struct{}

// Type 实现组件接口。
func (Heading) Type() string { return Type }

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

// isTypographyValue 排版值白名单：长度值或 clamp()。
func isTypographyValue(v string) bool {
	return len(v) <= 40 && (lenValueRe.MatchString(v) || clampRe.MatchString(v))
}

// Validate 校验标题节点。叶子组件。
func (Heading) Validate(node *core.Node, ids map[string]bool) (err error) {
	if !nodeIDValid(node.ID) {
		return fmt.Errorf("无效的节点 ID: %q", node.ID)
	}
	if ids[node.ID] {
		return fmt.Errorf("节点 ID 重复: %q", node.ID)
	}
	ids[node.ID] = true
	if len(node.Children) > 0 {
		return errors.New("标题组件为叶子节点，不允许子节点")
	}

	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// 内容：静态文本或绑定二选一。
	hasText := p.Text != ""
	hasBinding := p.Binding != nil && p.Binding.Field != ""
	if !hasText && !hasBinding {
		return fmt.Errorf("节点 %s: 必须提供静态文本或 CMS 绑定", node.ID)
	}
	if len(p.Text) > maxTextLen {
		return fmt.Errorf("节点 %s: 文本过长（上限 %d 字符）", node.ID, maxTextLen)
	}
	if p.Binding != nil && p.Binding.Field != "" {
		if !fieldPathRe.MatchString(p.Binding.Field) {
			return fmt.Errorf("节点 %s: 无效的绑定字段路径: %q", node.ID, p.Binding.Field)
		}
		if len(p.Binding.Fallback) > maxTextLen {
			return fmt.Errorf("节点 %s: 兜底文本过长", node.ID)
		}
	}

	// 语义标签。
	if p.Tag == "" {
		p.Tag = "h2"
	}
	if !allowedTags[p.Tag] {
		return fmt.Errorf("节点 %s: 无效的语义标签: %q", node.ID, p.Tag)
	}

	// 排版（三端）。
	for bp, t := range map[string]Typography{
		"desktop": p.Typography.Desktop, "tablet": p.Typography.Tablet, "mobile": p.Typography.Mobile,
	} {
		if t.FontSize != "" && !isTypographyValue(t.FontSize) {
			return fmt.Errorf("节点 %s: 无效的 %s 端字号: %q", node.ID, bp, t.FontSize)
		}
		if t.LineHeight != "" && !isTypographyValue(t.LineHeight) {
			return fmt.Errorf("节点 %s: 无效的 %s 端行高: %q", node.ID, bp, t.LineHeight)
		}
		if t.TextAlign != "" && !alignMap[t.TextAlign] {
			return fmt.Errorf("节点 %s: 无效的 %s 端对齐: %q", node.ID, bp, t.TextAlign)
		}
	}
	if _, err = resolveWeight(p.Weight); err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	if p.LetterSpacing != "" && (!core.IsSafeCSSValue(p.LetterSpacing) || len(p.LetterSpacing) > 20) {
		return fmt.Errorf("节点 %s: 无效的字间距: %q", node.ID, p.LetterSpacing)
	}
	if p.Transform != "" {
		if _, ok := transformMap[p.Transform]; !ok {
			return fmt.Errorf("节点 %s: 无效的文字转换: %q", node.ID, p.Transform)
		}
	}
	if p.Decor.Decoration != "" && !decorationMap[p.Decor.Decoration] {
		return fmt.Errorf("节点 %s: 无效的文本装饰: %q", node.ID, p.Decor.Decoration)
	}
	if p.Decor.DecorationColor != "" && !core.IsSafeCSSValue(p.Decor.DecorationColor) {
		return fmt.Errorf("节点 %s: 无效的装饰颜色: %q", node.ID, p.Decor.DecorationColor)
	}
	if p.Color != "" && !core.IsSafeCSSValue(p.Color) {
		return fmt.Errorf("节点 %s: 无效的文字颜色: %q", node.ID, p.Color)
	}
	if p.LineClamp < 0 || p.LineClamp > 6 {
		return fmt.Errorf("节点 %s: 截断行数必须在 1~6 之间: %d", node.ID, p.LineClamp)
	}
	if p.TextShadow != "" {
		if _, ok := textShadowPresets[p.TextShadow]; !ok {
			return fmt.Errorf("节点 %s: 无效的文字阴影: %q", node.ID, p.TextShadow)
		}
	}

	return core.ValidateAdvanced(&p.Advanced, node.ID, ids)
}

// nodeIDValid 节点 ID 校验。
func nodeIDValid(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// Render 渲染标题：单层语义标签，CMS 绑定发布期静态填入。
func (Heading) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.Tag == "" {
		p.Tag = "h2"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态文本。
	text := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if ctx.Content == nil {
			return fmt.Errorf("节点 %s: 编译上下文缺少内容解析器，无法解析绑定 %q", node.ID, p.Binding.Field)
		}
		v, err := ctx.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return fmt.Errorf("节点 %s: 解析绑定 %q 失败: %w", node.ID, p.Binding.Field, err)
		}
		if v != "" {
			text = v
		} else if p.Binding.Fallback != "" {
			text = p.Binding.Fallback
		}
	}

	// Advanced 层。
	extraClasses, customID := core.CompileAdvanced(node.ID, &p.Advanced, ctx.CSS)

	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(p.Tag)
	sb.WriteString(" class=\"")
	sb.WriteString(html.EscapeString(strings.Join(classes, " ")))
	sb.WriteString("\"")
	if customID != "" {
		sb.WriteString(" id=\"")
		sb.WriteString(html.EscapeString(customID))
		sb.WriteString("\"")
	}
	sb.WriteString(">")
	sb.WriteString(html.EscapeString(text))
	sb.WriteString("</")
	sb.WriteString(p.Tag)
	sb.WriteString(">")

	ctx.HTML.WriteString(sb.String())
	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// compileCSS 编译标题样式（三端排版 + 颜色/截断/阴影）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string

	// 排版三端。
	appendTypography := func(target *[]string, t Typography) {
		if t.FontSize != "" {
			*target = append(*target, "font-size: "+t.FontSize)
		}
		if t.LineHeight != "" {
			*target = append(*target, "line-height: "+t.LineHeight)
		}
		if t.TextAlign != "" {
			*target = append(*target, "text-align: "+t.TextAlign)
		}
	}
	appendTypography(&desktop, p.Typography.Desktop)
	appendTypography(&tablet, p.Typography.Tablet)
	appendTypography(&mobile, p.Typography.Mobile)

	// 字重 / 字间距 / 转换。
	if w, err := resolveWeight(p.Weight); err == nil && w != "" {
		desktop = append(desktop, "font-weight: "+w)
	}
	if v := p.LetterSpacing; v != "" {
		desktop = append(desktop, "letter-spacing: "+v)
	}
	if p.Transform != "" && p.Transform != "none" {
		desktop = append(desktop, "text-transform: "+transformMap[p.Transform])
	}

	// 装饰。
	if p.Decor.Decoration != "" && p.Decor.Decoration != "none" {
		decl := "text-decoration: " + p.Decor.Decoration
		if p.Decor.DecorationColor != "" {
			decl += " " + p.Decor.DecorationColor
		}
		desktop = append(desktop, decl)
	}

	// 颜色 / 文字阴影。
	if v := p.Color; v != "" {
		desktop = append(desktop, "color: "+v)
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
	core.Register(Heading{})
}