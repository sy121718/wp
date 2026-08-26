// Package text 实现 core.text 正文组件（规范 docs/02-C2）：
// 短说明、副标题描述、段落长文、产品详情等文本承载场景。
//
// 两种模式按需切换：
//   - 纯文本模式（plaintext）：输入轻量，直出单层 <p>/<span>，零多余嵌套；
//   - 富文本模式（richtext，默认）：结构化 HTML 片段（<p>/<ul>/<blockquote> 等），
//     编译期经白名单清洗（XSS 防护），行内超链接支持 target="_blank" 与 rel="nofollow"。
//
// CMS 字段绑定发布期静态填入（Fallback 兜底）；富文本绑定长文可选摘要模式（strip 标签截字）。
// 基础盒模型与通用样式继承自 02-C0 Advanced 层。
package text

import (
	"encoding/json"
	"errors"
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

// plaintextTag 纯文本允许的包裹标签。
var allowedPlainTags = map[string]bool{"p": true, "span": true}

// 常量上限。
const (
	maxPlainLen = 2000 // 纯文本长度上限
	maxExcerpt  = 400  // 摘要截取字符上限
	maxClamp    = 10   // line clamp 上限
	maxWeight   = 900  // 字重上限（文本不支持粗体时排版字重仍可用）
	minWeight   = 100  // 字重下限
	stepWeight  = 100  // 字重步进
)

// 排版组：core.TextStyle（groups.go 共享组，三端字号/行高/对齐）。

// Binding 字段绑定。
type Binding struct {
	Field    string `json:"field,omitempty"`    // 字段路径：post.excerpt / category.description / post.content 等
	Fallback string `json:"fallback,omitempty"` // 绑定字段为空时的兜底文本
}

// Props core.text 特有属性 + Advanced 通用层。
type Props struct {
	// Mode 内容模式：richtext（默认）/ plaintext。
	Mode string `json:"mode,omitempty"`
	// PlainTag 纯文本模式的包裹标签：p（默认）/ span。
	PlainTag string `json:"plainTag,omitempty"`
	// Text 内容：纯文本模式为纯字符串；富文本模式为 HTML 片段（编译期白名单清洗）。
	Text string `json:"text,omitempty"`
	// Binding CMS 字段绑定（优先于 Text）。
	Binding *Binding `json:"binding,omitempty"`
	// Typography 基准字号行高与对齐（三端独立，core.TextStyle 共享组）。
	Typography core.TextStyle `json:"typography,omitempty"`
	// ParagraphSpacing 段间距（富文本模式下的段落上下留白）。
	ParagraphSpacing string `json:"paragraphSpacing,omitempty"`
	// Color 文字颜色（色值或主题 Token）。
	Color string `json:"color,omitempty"`
	// LinkColor 链接颜色（色值或主题 Token，富文本模式）。
	LinkColor string `json:"linkColor,omitempty"`
	// LineClamp 多行截断 1~10；0 关闭。
	LineClamp int `json:"lineClamp,omitempty"`
	// Excerpt 富文本绑定长文时仅取纯文本截前 N 字；0 关闭（仅 binding 时生效）。
	Excerpt int `json:"excerpt,omitempty"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Text core.text 组件实现。
type Text struct{}

// Type 实现组件接口。
func (Text) Type() string { return Type }

// regexpCompile 编译正则（包级白名单）。
func regexpCompile(pat string) *regexp.Regexp {
	return regexp.MustCompile(pat)
}

// Validate 校验正文节点。叶子组件。
func (Text) Validate(node *core.Node, ids map[string]bool) (err error) {
	if !nodeIDValid(node.ID) {
		return fmt.Errorf("无效的节点 ID: %q", node.ID)
	}
	if ids[node.ID] {
		return fmt.Errorf("节点 ID 重复: %q", node.ID)
	}
	ids[node.ID] = true
	if len(node.Children) > 0 {
		return errors.New("正文组件为叶子节点，不允许子节点")
	}

	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// 模式与内容。
	if p.Mode == "" {
		p.Mode = ModeRichText
	}
	if p.Mode != ModeRichText && p.Mode != ModePlainText {
		return fmt.Errorf("节点 %s: 无效的内容模式: %q", node.ID, p.Mode)
	}
	if p.Mode == ModePlainText {
		if p.PlainTag == "" {
			p.PlainTag = "p"
		}
		if !allowedPlainTags[p.PlainTag] {
			return fmt.Errorf("节点 %s: 无效的纯文本标签: %q", node.ID, p.PlainTag)
		}
		plain := p.Text
		if p.Binding != nil {
			plain = p.Binding.Fallback
			_ = plain
		}
		if len(p.Text) > maxPlainLen {
			return fmt.Errorf("节点 %s: 纯文本过长（上限 %d 字符）", node.ID, maxPlainLen)
		}
		if p.Binding != nil && len(p.Binding.Fallback) > maxPlainLen {
			return fmt.Errorf("节点 %s: 兜底文本过长（上限 %d 字符）", node.ID, maxPlainLen)
		}
	} else {
		if len(p.Text) > 30000 {
			return fmt.Errorf("节点 %s: 富文本过长（上限 30000 字符）", node.ID)
		}
		if p.Binding != nil && len(p.Binding.Fallback) > maxPlainLen {
			return fmt.Errorf("节点 %s: 兜底文本过长（上限 %d 字符）", node.ID, maxPlainLen)
		}
	}
	if p.Text == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("节点 %s: 必须提供内容或 CMS 绑定", node.ID)
	}
	if p.Binding != nil && p.Binding.Field != "" && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("节点 %s: 无效的绑定字段路径: %q", node.ID, p.Binding.Field)
	}

	// 排版三端（共享组校验 core.TextStyle）。
	if err = core.ValidateTextStyle(node.ID, &p.Typography); err != nil {
		return err
	}

	// 段落间距 / 颜色 / 链接色。
	if p.ParagraphSpacing != "" && !core.IsSafeCSSValue(p.ParagraphSpacing) {
		return fmt.Errorf("节点 %s: 无效的段间距: %q", node.ID, p.ParagraphSpacing)
	}
	if p.Color != "" && !core.IsSafeCSSValue(p.Color) {
		return fmt.Errorf("节点 %s: 无效的文字颜色: %q", node.ID, p.Color)
	}
	if p.LinkColor != "" && !core.IsSafeCSSValue(p.LinkColor) {
		return fmt.Errorf("节点 %s: 无效的链接颜色: %q", node.ID, p.LinkColor)
	}

	// 截断与摘要。
	if p.LineClamp < 0 || p.LineClamp > maxClamp {
		return fmt.Errorf("节点 %s: 截断行数必须在 1~%d 之间: %d", node.ID, maxClamp, p.LineClamp)
	}
	if p.Excerpt < 0 || p.Excerpt > maxExcerpt {
		return fmt.Errorf("节点 %s: 摘要长度必须在 1~%d 之间: %d", node.ID, maxExcerpt, p.Excerpt)
	}
	if p.Excerpt > 0 && p.Mode != ModeRichText {
		return fmt.Errorf("节点 %s: 摘要模式仅限富文本绑定场景", node.ID)
	}

	return core.ValidateAdvanced(&p.Advanced, node.ID, ids)
}

// nodeIDValid 节点 ID 校验（与 other 组件一致）。
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

// fieldPathRe 绑定字段路径白名单（同 02-C1）。
var fieldPathRe = regexpCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// init 注册正文组件。
func init() {
	core.Register(Text{})
}

// Render 渲染正文：纯文本单层直出 / 富文本结构化片段（白名单已清洗）。
func (Text) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.Mode == "" {
		p.Mode = ModeRichText
	}
	if p.Mode == ModePlainText && p.PlainTag == "" {
		p.PlainTag = "p"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态内容。
	content := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if ctx.Content == nil {
			return fmt.Errorf("节点 %s: 编译上下文缺少内容解析器，无法解析绑定 %q", node.ID, p.Binding.Field)
		}
		v, err := ctx.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return fmt.Errorf("节点 %s: 解析绑定 %q 失败: %w", node.ID, p.Binding.Field, err)
		}
		if v != "" {
			content = v
		} else if p.Binding.Fallback != "" {
			content = p.Binding.Fallback
		}
	}

	// Advanced 层。
	extraClasses, customID := core.CompileAdvanced(node.ID, &p.Advanced, ctx.CSS)
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)
	classAttr := html.EscapeString(strings.Join(classes, " "))

	var sb strings.Builder
	sb.WriteString(`<div class="`)
	sb.WriteString(classAttr)
	sb.WriteString(`"`)
	if customID != "" {
		sb.WriteString(` id="`)
		sb.WriteString(html.EscapeString(customID))
		sb.WriteString(`"`)
	}
	sb.WriteString(">")

	switch p.Mode {
	case ModePlainText:
		contentPlain := content
		if p.Binding != nil && p.Binding.Field != "" && content == p.Text && p.Text != "" {
			// 无绑定变化
		}
		sb.WriteString("<")
		sb.WriteString(p.PlainTag)
		sb.WriteString(">")
		sb.WriteString(html.EscapeString(contentPlain))
		sb.WriteString("</")
		sb.WriteString(p.PlainTag)
		sb.WriteString(">")
	case ModeRichText:
		rich := sanitizeRichHTML(content)
		if p.Excerpt > 0 {
			// 摘要模式：strip 全部标签取纯文本，截前 N 字符。
			rich = html.EscapeString(truncateRunes(stripRichTags(content), p.Excerpt))
		}
		sb.WriteString(rich)
	}

	sb.WriteString("</div>")

	ctx.HTML.WriteString(sb.String())
	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// truncateRunes 按字符数截断。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// compileCSS 编译正文样式（三端排版 + 段落间距 + 颜色/链接色 + 截断）。
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

	// 段落间距：富文本模式下作用于内部块级元素（p/ul/blockquote）。
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
