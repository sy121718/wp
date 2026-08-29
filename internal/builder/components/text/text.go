// Package text 实现 core.text 正文组件（规范 docs/02-C2、02-C5）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：纯文本/富文本双模式、
// CMS 绑定与摘要、富文本白名单清洗、段间距/颜色/链接色/截断样式编译。
package text

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.text"

// 内容模式常量。
const (
	ModeRichText  = "richtext"  // 富文本模式（默认）
	ModePlainText = "plaintext" // 纯文本模式
)

// 常量上限。
const (
	maxPlainLen = 2000 // 纯文本长度上限
	maxExcerpt  = 400  // 摘要截取字符上限
	maxClamp    = 10   // line clamp 上限
)

// Binding 字段绑定。
type Binding struct {
	Field    string `json:"field,omitempty"`    // 字段路径：post.excerpt / category.description / post.content 等
	Fallback string `json:"fallback,omitempty"` // 绑定字段为空时的兜底文本
}

// Props core.text 特有属性 + 共享排版组 + Advanced 通用层。
type Props struct {
	// Mode 内容模式：richtext（默认）/ plaintext。
	Mode string `json:"mode,omitempty" ct:"select,richtext=富文本,plaintext=纯文本,default=richtext,sec=content,label=内容模式"`
	// PlainTag 纯文本模式的包裹标签：p（默认）/ span。
	PlainTag string `json:"plainTag,omitempty" ct:"select,p=段落,span=行内,default=p,sec=content,label=包裹标签"`
	// Text 内容：纯文本模式为纯字符串；富文本模式为 HTML 片段（编译期白名单清洗）。
	Text string `json:"text,omitempty" ct:"text,maxlen=30000,sec=content"`
	// Binding CMS 字段绑定（优先于 Text）。
	Binding *Binding `json:"binding,omitempty"`
	// Typography 基准字号行高与对齐（三端独立，core.TextStyle 共享组）。
	Typography core.TextStyle `json:"typography,omitempty"`
	// ParagraphSpacing 段间距（富文本模式下的段落上下留白）。
	ParagraphSpacing string `json:"paragraphSpacing,omitempty" ct:"safe,maxlen=30,sec=style"`
	// Color 文字颜色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"safe,maxlen=200,sec=style"`
	// LinkColor 链接颜色（色值或主题 Token，富文本模式）。
	LinkColor string `json:"linkColor,omitempty" ct:"safe,maxlen=200,sec=style"`
	// LineClamp 多行截断 1~10；0 关闭。
	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=10,step=1,sec=style"`
	// Excerpt 富文本绑定长文时仅取纯文本截前 N 字；0 关闭（仅 binding 时生效）。
	Excerpt int `json:"excerpt,omitempty" ct:"slider,min=0,max=400,step=5,sec=style"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		Render:        render,
	},
}

// fieldPathRe 绑定字段路径白名单。
var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

// validateExtra 关系性校验：绑定路径、富文本长度、摘要模式限制、排版组。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Binding != nil && p.Binding.Field != "" {
		if !fieldPathRe.MatchString(p.Binding.Field) {
			return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
		}
		if len(p.Binding.Fallback) > maxPlainLen {
			return fmt.Errorf("兜底文本过长（上限 %d 字符）", maxPlainLen)
		}
	}
	if p.Mode == ModePlainText && len(p.Text) > maxPlainLen {
		return fmt.Errorf("纯文本过长（上限 %d 字符）", maxPlainLen)
	}
	if p.Mode == ModeRichText && len(p.Text) > 30000 {
		return fmt.Errorf("富文本过长（上限 30000 字符）")
	}
	if p.Text == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("必须提供内容或 CMS 绑定")
	}
	if p.Excerpt > 0 && p.Mode != ModeRichText {
		return fmt.Errorf("摘要模式仅限富文本绑定场景")
	}
	return core.ValidateTextStyle(nodeID, &p.Typography)
}

// render 内容解析（绑定/Fallback/静态内容）→ 纯文本转义 / 富文本清洗摘要 → 包裹输出。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	if p.Mode == "" {
		p.Mode = ModeRichText
	}
	if p.Mode == ModePlainText && p.PlainTag == "" {
		p.PlainTag = "p"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态内容。
	content := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if h.Content == nil {
			return "", fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := h.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			content = v
		} else if p.Binding.Fallback != "" {
			content = p.Binding.Fallback
		}
	}

	var sb strings.Builder
	sb.WriteString(`<div class="`)
	sb.WriteString(h.Classes)
	sb.WriteString(`"`)
	if h.CustomID != "" {
		sb.WriteString(` id="`)
		sb.WriteString(h.CustomID)
		sb.WriteString(`"`)
	}
	sb.WriteString(">")

	switch p.Mode {
	case ModePlainText:
		sb.WriteString("<")
		sb.WriteString(p.PlainTag)
		sb.WriteString(">")
		sb.WriteString(html.EscapeString(content))
		sb.WriteString("</")
		sb.WriteString(p.PlainTag)
		sb.WriteString(">")
	case ModeRichText:
		rich := sanitizeRichHTML(content)
		if p.Excerpt > 0 {
			rich = html.EscapeString(truncateRunes(stripRichTags(content), p.Excerpt))
		}
		sb.WriteString(rich)
	}
	sb.WriteString("</div>")

	compileCSS(node.ID, p, h.CSS)
	return sb.String(), nil
}

// truncateRunes 按字符数截断（摘要）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// compileCSS 正文样式：排版组三端 + 颜色/链接色 + 段间距 + 截断。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string
	desktop = append(desktop, p.Typography.BreakpointDecls(core.BreakpointDesktop)...)
	tablet = append(tablet, p.Typography.BreakpointDecls(core.BreakpointTablet)...)
	mobile = append(mobile, p.Typography.BreakpointDecls(core.BreakpointMobile)...)

	if p.Color != "" {
		desktop = append(desktop, "color: "+p.Color)
	}
	if p.LinkColor != "" {
		desktop = append(desktop, "color: "+p.LinkColor)
	}

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointTablet, sel, tablet)
	b.Add(core.BreakpointMobile, sel, mobile)

	// 段间距：富文本模式下内部块级元素。
	if p.ParagraphSpacing != "" {
		b.Add(core.BreakpointDesktop, sel+" p, "+sel+" ul, "+sel+" blockquote", []string{
			"margin-top: " + p.ParagraphSpacing,
			"margin-bottom: " + p.ParagraphSpacing,
		})
	}

	// 多行截断（含省略号）。
	if p.LineClamp > 0 {
		b.Add(core.BreakpointDesktop, sel, []string{
			"display: -webkit-box",
			fmt.Sprintf("-webkit-line-clamp: %d", p.LineClamp),
			"-webkit-box-orient: vertical",
			"overflow: hidden",
		})
	}
}

// init 注册正文组件。
func init() {
	core.Register(Widget)
}
