// Package heading 实现 core.heading 标题组件（规范 docs/02-C1、02-C5）。
// 基座 core.Atom 吸收公共样板（ID/叶子/解码/声明式校验/Advanced/class 织入）。
// 本文件为业务本体：文本或 CMS 绑定、语义标签、排版（共享组 TextStyle）、
// 字重/字间距/转换/装饰/颜色/截断/阴影与对应样式编译。
package heading

import (
	"fmt"
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
	Decoration string `json:"decoration,omitempty" ct:"select,none=无,underline=下划线,line-through=删除线,label=文字装饰"`
	// DecorationColor 装饰线颜色微调。
	DecorationColor string `json:"decorationColor,omitempty" ct:"color,maxlen=100"`
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
	Tag string `json:"tag,omitempty" ct:"select,h1=一级标题,h2=二级标题,h3=三级标题,h4=四级标题,h5=五级标题,h6=六级标题,div=区块,span=行内,default=h2,sec=content,label=语义标签"`
	// Typography 字体排版（三端独立，core.TextStyle 共享组）。
	Typography core.TextStyle `json:"typography,omitempty"`
	// Weight 字重：100~900 或 token（regular/medium/semibold/bold）。
	Weight string `json:"weight,omitempty" ct:"string,maxlen=10,sec=style"`
	// LetterSpacing 字间距。
	LetterSpacing string `json:"letterSpacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
	// Transform 文字转换：none/uppercase/lowercase/capitalize。
	Transform string `json:"transform,omitempty" ct:"select,none=无,uppercase=全大写,lowercase=全小写,capitalize=首字母大写,sec=style,label=大小写转换"`
	// Decor 文本装饰。
	Decor DecorProps `json:"decor,omitempty"`
	// Color 文字颜色：色值或主题 Token（var(--wp-c-primary)）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	// LineClamp 多行截断行数 1~6；0 表示不截断。
	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=6,step=1,sec=style"`
	// TextShadow 文字阴影预设：subtle/strong；空为无。
	TextShadow string `json:"textShadow,omitempty" ct:"select,subtle=轻阴影,strong=重阴影,sec=style,label=文字阴影"`
	// TextStroke 文字描边预设：thin 细描边 / bold 粗描边（currentColor；
	// 效果基本库 core.TextStrokeDecls，分类目录文本 FX）；空为无。
	TextStroke string `json:"textStroke,omitempty" ct:"select,thin=细描边,bold=粗描边,sec=style,label=文字描边"`
	// Subtitle 副标题文本（显示于主标题之上，小字）。清空保存空串，渲染端
	// 空串不输出 <span>（无空标签残留）。
	Subtitle string `json:"subtitle,omitempty" ct:"text,maxlen=200,sec=content,label=副标题"`
	// SubtitleColor 副标题颜色。
	SubtitleColor string `json:"subtitleColor,omitempty" ct:"color,maxlen=200,sec=style,label=副标题颜色"`
	// SubtitleFontSize 副标题字号（如 "14px"），空=默认 0.875em。
	SubtitleFontSize string `json:"subtitleFontSize,omitempty" ct:"dimension,maxlen=30,sec=style,label=副标题字号"`
	// SubtitleFontWeight 副标题字重。
	SubtitleFontWeight string `json:"subtitleFontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,default=600,sec=style,label=副标题字重"`
	// SubtitleSpacing 副标题与主标题的间距（如 "8px"）。
	SubtitleSpacing string `json:"subtitleSpacing,omitempty" ct:"dimension,maxlen=30,sec=style,label=副标题间距"`
	// Align 三端对齐：left/center/right。
	Align Align `json:"align,omitempty" ct:"rtext,sec=layout,label=对齐"`
	// Width 宽度（CSS 长度，三端）。
	Width Responsive `json:"width,omitempty" ct:"rtext,sec=layout,label=宽度"`
	// Highlight 高亮背景盒（WD 标题高亮装饰）：背景色/内边距/圆角。
	HighlightColor   string `json:"highlightColor,omitempty" ct:"color,maxlen=200,sec=style,label=高亮背景色"`
	HighlightPadding string `json:"highlightPadding,omitempty" ct:"dimension,maxlen=30,sec=style,label=高亮内边距"`
	HighlightRadius  string `json:"highlightRadius,omitempty" ct:"dimension,maxlen=30,sec=style,label=高亮圆角"`
	// TextAnim 文本动画（字/词错落入场）："" 无 / chars 逐字 / words 逐词。
	// 与高亮盒（Highlight）互不叠加——套高亮盒时不做拆分动画。
	TextAnim string `json:"textAnim,omitempty" ct:"select,=无,chars=逐字,words=逐词,sec=style,label=文本动画"`
	// TextAnimDelay 字间延迟（毫秒，10~200，默认 40）。
	TextAnimDelay int `json:"textAnimDelay,omitempty" ct:"int,min=10,max=200,sec=style,label=字间延迟(ms)"`
	// Advanced 通用高级属性（规范 docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Align 三端对齐。
type Align struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Responsive 三端值。
type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"text", "subtitle"},
	},
}

// 字重 Token 与文字阴影预设。
var (
	weightTokenMap    = map[string]int{"regular": 400, "medium": 500, "semibold": 600, "bold": 700}
	textShadowPresets = map[string]string{
		"subtle": "0 1px 2px rgba(0,0,0,0.45)",
		"strong": "0 2px 6px rgba(0,0,0,0.6)",
	}
	// decorMap 装饰白名单（ct select 外的手写映射作用于渲染取值）。
	decorationMap = map[string]bool{"none": true, "underline": true, "line-through": true}
	// fieldPathRe 绑定字段路径白名单。
	fieldPathRe = regexpCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
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
	for bp, w := range map[string]string{"desktop": p.Width.Desktop, "tablet": p.Width.Tablet, "mobile": p.Width.Mobile} {
		if w != "" && !core.IsSafeCSSValue(w) {
			return fmt.Errorf("无效的 %s 端宽度: %q", bp, w)
		}
	}
	return core.ValidateTextStyle(nodeID, &p.Typography)
}

// textAnimOn 文本动画是否生效：开启且未套高亮盒。
// 与 BuildView 共用同一判定（单一真源）——高亮盒模式下模板不拆分文本，
// CSS 侧同步不产出分段规则，产物里不会留下永不匹配的死规则。
func textAnimOn(p *Props) bool {
	return p.TextAnim != "" && p.HighlightColor == ""
}

// compileCSS 标题样式：排版组三端声明 + 字重/间距/转换/装饰/颜色/截断/阴影。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string
	desktop = append(desktop, p.Typography.BreakpointDecls(core.BreakpointDesktop)...)
	tablet = append(tablet, p.Typography.BreakpointDecls(core.BreakpointTablet)...)
	mobile = append(mobile, p.Typography.BreakpointDecls(core.BreakpointMobile)...)

	if w, err := resolveWeight(p.Weight); err == nil && w != "" {
		desktop = append(desktop, core.CSSDecl("font-weight", w))
	}
	if p.LetterSpacing != "" {
		desktop = append(desktop, core.CSSDecl("letter-spacing", p.LetterSpacing))
	}
	if p.Transform != "" && p.Transform != "none" {
		desktop = append(desktop, core.CSSDecl("text-transform", p.Transform))
	}
	if p.Decor.Decoration != "" && p.Decor.Decoration != "none" {
		decl := core.CSSDecl("text-decoration", p.Decor.Decoration)
		if p.Decor.DecorationColor != "" {
			decl += " " + p.Decor.DecorationColor
		}
		desktop = append(desktop, decl)
	}
	if p.Color != "" {
		desktop = append(desktop, core.CSSDecl("color", p.Color))
	}
	if v, ok := textShadowPresets[p.TextShadow]; p.TextShadow != "" && ok {
		desktop = append(desktop, core.CSSDecl("text-shadow", v))
	}
	// 标题平衡换行（现代 CSS，H5 窄屏长标题观感提升；不支持的浏览器自动忽略）。
	desktop = append(desktop, "text-wrap: balance")
	// 文字描边（效果基本库 core.TextStrokeDecls，分类目录文本 FX）。
	switch p.TextStroke {
	case "thin":
		desktop = append(desktop, core.TextStrokeDecls("1px", "currentColor")...)
	case "bold":
		desktop = append(desktop, core.TextStrokeDecls("2px", "currentColor")...)
	}

	// 副标题样式（默认基底 + 用户覆盖：颜色/字号/字重/间距）。
	subDecls := []string{
		"display: block", "font-size: 0.62em", "font-weight: 500",
		"letter-spacing: 0.08em", "text-transform: uppercase",
		"margin-bottom: 0.4em", "opacity: .7",
	}
	if p.SubtitleFontSize != "" {
		subDecls = append(subDecls, core.CSSDecl("font-size", p.SubtitleFontSize), "opacity: 1")
	}
	if p.SubtitleFontWeight != "" {
		subDecls = append(subDecls, core.CSSDecl("font-weight", p.SubtitleFontWeight))
	}
	if p.SubtitleSpacing != "" {
		subDecls = append(subDecls, core.CSSDecl("margin-bottom", p.SubtitleSpacing))
	}
	// 副标题在 DOM 中是根元素的**前置兄弟**（heading.jet：subtitle 在标题标签之前），
	// 因此后代选择器 sel+" .wp-heading-sub" 永远匹配不到任何元素 —— 副标题的颜色、
	// 字号、字重、间距此前全部静默失效。用 :has() 从副标题侧反向限定到本节点。
	subSel := ".wp-heading-sub:has(+ " + sel + ")"
	b.Add(core.BreakpointDesktop, subSel, subDecls)
	if p.SubtitleColor != "" {
		b.Add(core.BreakpointDesktop, subSel, []string{core.CSSDecl("color", p.SubtitleColor), "opacity: 1"})
	}
	// 高亮背景盒。
	if p.HighlightColor != "" {
		hl := []string{core.CSSDecl("background", p.HighlightColor)}
		if p.HighlightPadding != "" {
			hl = append(hl, core.CSSDecl("padding", p.HighlightPadding))
		}
		if p.HighlightRadius != "" {
			hl = append(hl, core.CSSDecl("border-radius", p.HighlightRadius))
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-heading-highlight", hl)
	}
	// 对齐与宽度（三端）。
	appendAlign := func(target *[]string, a string) {
		switch a {
		case "left":
			*target = append(*target, "text-align: left")
		case "center":
			*target = append(*target, "text-align: center")
		case "right":
			*target = append(*target, "text-align: right")
		}
	}
	appendAlign(&desktop, p.Align.Desktop)
	appendAlign(&tablet, p.Align.Tablet)
	appendAlign(&mobile, p.Align.Mobile)
	appendWidth := func(target *[]string, w string) {
		if w != "" && core.IsSafeCSSValue(w) {
			*target = append(*target, core.CSSDecl("width", w))
		}
	}
	appendWidth(&desktop, p.Width.Desktop)
	appendWidth(&tablet, p.Width.Tablet)
	appendWidth(&mobile, p.Width.Mobile)

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointTablet, sel, tablet)
	b.Add(core.BreakpointMobile, sel, mobile)

	// 多行截断：-webkit-box 标准组合。
	// 文本动画（逐字/逐词错落入场）：分段 span 自左向右递增延迟。
	// 前 20 段逐段递增；第 21 段起用统一档位兜底（避免为长标题生成大量规则）。
	if textAnimOn(p) {
		delay := p.TextAnimDelay
		if delay <= 0 {
			delay = 40
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-h-seg", []string{
			"display: inline-block",
			"white-space: pre",
			"animation: wp-fade-up 0.6s ease backwards",
		})
		for i := 1; i <= 20; i++ {
			b.Add(core.BreakpointDesktop, fmt.Sprintf("%s .wp-h-seg:nth-child(%d)", sel, i),
				[]string{fmt.Sprintf("animation-delay: %dms", (i-1)*delay)})
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-h-seg:nth-child(n+21)",
			[]string{fmt.Sprintf("animation-delay: %dms", 20*delay)})
		b.NeedKeyframes("wp-fade-up")
	}

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
