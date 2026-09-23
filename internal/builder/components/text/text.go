// Package text 实现 core.text 正文组件（规范 docs/02-C2、02-C5）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：纯文本/富文本双模式、
// CMS 绑定与摘要、富文本白名单清洗、段间距/颜色/链接色/截断样式编译。
package text

import (
	_ "embed" // text.css 经 //go:embed 打进二进制
	"fmt"
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
	Field    string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"` // 字段路径：item.excerpt（集合项）/ post.excerpt / category.description 等
	Fallback string `json:"fallback,omitempty"`                                                 // 绑定字段为空时的兜底文本
}

// Props core.text 特有属性 + 共享排版组 + Advanced 通用层。
type Props struct {
	// Mode 内容模式：richtext（默认）/ plaintext。
	Mode string `json:"mode,omitempty" ct:"select,richtext=富文本,plaintext=纯文本,default=richtext,sec=content,label=内容模式"`
	// PlainTag 纯文本模式的包裹标签：p（默认）/ span。
	PlainTag string `json:"plainTag,omitempty" ct:"select,p=段落,span=行内,default=p,sec=content,label=包裹标签"`
	// Text 内容：纯文本模式为纯字符串；富文本模式为 HTML 片段（编译期白名单清洗）。
	// ct kind = richtext：检查器走 Trix 富文本编辑器（Mode=plaintext 时前端回退多行输入）。
	Text string `json:"text,omitempty" ct:"richtext,maxlen=30000,sec=content"`
	// Binding CMS 字段绑定（优先于 Text）。
	Binding *Binding `json:"binding,omitempty"`
	// Typography 基准字号行高与对齐（三端独立，core.TextStyle 共享组）。
	Typography core.TextStyle `json:"typography,omitempty"`
	// ParagraphSpacing 段间距（富文本模式下的段落上下留白）。
	ParagraphSpacing string `json:"paragraphSpacing,omitempty" ct:"dimension,maxlen=30,sec=style"`
	// Color 文字颜色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	// LinkColor 链接颜色（色值或主题 Token，富文本模式）。
	LinkColor string `json:"linkColor,omitempty" ct:"color,maxlen=200,sec=style"`
	// LineClamp 多行截断 1~10；0 关闭。
	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=10,step=1,sec=style"`
	// Excerpt 富文本绑定长文时仅取纯文本截前 N 字；0 关闭（仅 binding 时生效）。
	Excerpt int `json:"excerpt,omitempty" ct:"slider,min=0,max=400,step=5,sec=style"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "文本",
		Hint:            "正文段落",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"mode":     "plaintext",
			"plainTag": "p",
			"text":     "在这里输入正文内容。",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"text"},
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

// truncateRunes 按字符数截断（摘要）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// textCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed text.css
var textCSS string

// typoDecls 把某一端的排版组声明拼成可交给样式源的「多条声明」字符串。
// core.Typography 的产出是一组声明（可能为空），这里不做筛选，空组自然整组不产出。
func typoDecls(p *Props, bp string) string {
	return strings.Join(p.Typography.BreakpointDecls(bp), "; ")
}

// compileCSS 正文样式：排版组三端 + 颜色/链接色 + 段间距 + 截断。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	clamp := ""
	if p.LineClamp > 0 {
		clamp = fmt.Sprintf("%d", p.LineClamp)
	}
	vars := map[string]string{
		"typo_desktop": typoDecls(p, core.BreakpointDesktop),
		"typo_tablet":  typoDecls(p, core.BreakpointTablet),
		"typo_mobile":  typoDecls(p, core.BreakpointMobile),
		"color":        p.Color,
		"link_color":   p.LinkColor,
		"para_spacing": effectiveParagraphSpacing(p),
		"clamp":        clamp,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, textCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("text 组件样式解析失败: %v", err))
	}
	// 富文本排版基线单独一份样式源、按需应用。
	//
	// 为什么不写成 text.css 里的一段 @if：样式引擎的 @if 只支持**声明块内**的条件段
	// （见 badge.css），包不住「一整组顶层规则」，硬写会在构建期以
	// 「看不懂这一行 "*"」炸掉。拆成两个文件后条件判断回到 Go，两份样式各自线性可读。
	if isRichText(p) {
		if err := core.ApplyComponentCSSTmpl(b, sel, textRichCSS, nil); err != nil {
			panic(fmt.Sprintf("text 富文本样式解析失败: %v", err))
		}
	}
}

// effectiveParagraphSpacing 段间距：作者配了就用，没配兜底 1em。
//
// 为什么必须有兜底：富文本正文是从 CMS 灌进来的，作者多半不会为「一篇文章」
// 单独调段间距；空值会让样式引擎把整条 margin 声明省略 —— 于是段与段贴在一起，
// 读起来是一块灰墙（实测踩过）。给的是下界而不是设计上限：作者仍可覆盖。
func effectiveParagraphSpacing(p *Props) string {
	if p != nil {
		if v := strings.TrimSpace(p.ParagraphSpacing); v != "" {
			return v
		}
	}
	// 兜底只给「真的会渲染富文本」的实例：什么都没配的正文组件产出空 <div>，
	// 不该为此输出一条段间距规则（不产出死 CSS，text_css_test.go 钉住这条）。
	if !isRichText(p) {
		return ""
	}
	return defaultParagraphSpacing
}

// isRichText 该实例是否真的会渲染富文本内容。
//
// 判据 = 「模式不是纯文本」且「有静态内容或字段绑定」。它门控的是**富文本排版基线**
// （h1~h6 边距、列表缩进、表格、img 的 max-width 等）：这些规则只在真有一段富文本
// 要排的时候才有意义，全空实例不该输出它们。
func isRichText(p *Props) bool {
	if p == nil {
		return false
	}
	if p.Mode == ModePlainText {
		return false
	}
	return strings.TrimSpace(p.Text) != "" || p.Binding != nil
}

// defaultParagraphSpacing 段间距兜底值。
const defaultParagraphSpacing = "1em"

// init 注册正文组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("text", textTemplate)
}

// textRichCSS 富文本排版基线（只在实例真的渲染富文本时应用）。
//
//go:embed text_richtext.css
var textRichCSS string

// textTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed text.jet
var textTemplate string
