// Package heading 实现 core.heading 标题组件（规范 docs/02-C1、02-C5）。
// 基座 core.Atom 吸收公共样板（ID/叶子/解码/声明式校验/Advanced/class 织入）。
// 本文件为业务本体：文本或 CMS 绑定、语义标签、排版（共享组 TextStyle）、
// 字重/字间距/转换/装饰/颜色/截断/阴影与对应样式编译。
package heading

import (
	_ "embed" // heading.css 经 //go:embed 打进二进制
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
	Field string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"`
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
	// Color 文字颜色：色值或主题 Token（var(--sky-c-primary)）。
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

// headingCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed heading.css
var headingCSS string

// typoDecls 把某一端的排版组声明拼成可交给样式源的「多条声明」字符串。
func typoDecls(p *Props, bp string) string {
	return strings.Join(p.Typography.BreakpointDecls(bp), "; ")
}

// boolVar 条件段变量的真值形态（非空即真）。
func boolVar(v bool) string {
	if v {
		return "1"
	}
	return ""
}

// segDelay 分段延迟值：未开启文本动画时返回空串（空值让对应声明省略）。
func segDelay(on bool, ms int) string {
	if !on {
		return ""
	}
	return fmt.Sprintf("%dms", ms)
}

// compileCSS 标题样式：排版组三端声明 + 字重/间距/转换/装饰/颜色/截断/阴影。
//
// 与迁移前的差别只在「值怎么算、声明怎么拼」：值与分支判定仍在 Go（白名单、字号预设、
// 描边库、逐段延迟），属性的组合与可选性全部搬进 heading.css。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	weight := ""
	if w, err := resolveWeight(p.Weight); err == nil {
		weight = w
	}
	transform := ""
	if p.Transform != "" && p.Transform != "none" {
		transform = p.Transform
	}
	decoration := ""
	if p.Decor.Decoration != "" && p.Decor.Decoration != "none" {
		decoration = core.CSSDecl("text-decoration", p.Decor.Decoration)
		if p.Decor.DecorationColor != "" {
			decoration += " " + p.Decor.DecorationColor
		}
	}
	textShadow := ""
	if v, ok := textShadowPresets[p.TextShadow]; p.TextShadow != "" && ok {
		textShadow = v
	}
	// 文字描边（效果基本库 core.TextStrokeDecls，分类目录文本 FX）—— 返回一组声明。
	stroke := ""
	switch p.TextStroke {
	case "thin":
		stroke = strings.Join(core.TextStrokeDecls("1px", "currentColor"), "; ")
	case "bold":
		stroke = strings.Join(core.TextStrokeDecls("2px", "currentColor"), "; ")
	}

	alignOf := func(a string) string {
		switch a {
		case "left", "center", "right":
			return a
		}
		return ""
	}
	widthOf := func(w string) string {
		if w != "" && core.IsSafeCSSValue(w) {
			return w
		}
		return ""
	}

	anim := textAnimOn(p)
	delay := p.TextAnimDelay
	if delay <= 0 {
		delay = 40
	}
	clamp := ""
	if p.LineClamp > 0 {
		clamp = strconv.Itoa(p.LineClamp)
	}

	vars := map[string]string{
		// scope 供 @global 规则把「副标题的前置兄弟」限定到本实例。
		"scope":           sel,
		"typo_desktop":    typoDecls(p, core.BreakpointDesktop),
		"typo_tablet":     typoDecls(p, core.BreakpointTablet),
		"typo_mobile":     typoDecls(p, core.BreakpointMobile),
		"weight":          weight,
		"letter_spacing":  p.LetterSpacing,
		"transform":       transform,
		"decoration":      decoration,
		"color":           p.Color,
		"text_shadow":     textShadow,
		"stroke":          stroke,
		"align_desktop":   alignOf(p.Align.Desktop),
		"align_tablet":    alignOf(p.Align.Tablet),
		"align_mobile":    alignOf(p.Align.Mobile),
		"width_desktop":   widthOf(p.Width.Desktop),
		"width_tablet":    widthOf(p.Width.Tablet),
		"width_mobile":    widthOf(p.Width.Mobile),
		"hl_color":        p.HighlightColor,
		"hl_padding":      p.HighlightPadding,
		"hl_radius":       p.HighlightRadius,
		"sub_font_size":   p.SubtitleFontSize,
		"sub_font_weight": p.SubtitleFontWeight,
		"sub_spacing":     p.SubtitleSpacing,
		"sub_color":       p.SubtitleColor,
		"text_anim":       boolVar(anim),
		"clamp":           clamp,
	}
	// 逐段延迟：前 20 段逐段递增；第 21 段起用统一档位兜底（避免为长标题生成大量规则）。
	// 未开启文本动画时全部留空 —— 空值让那 21 条延迟规则整体不产出（迁移前是 if 包住整段）。
	for i := 1; i <= 20; i++ {
		vars[fmt.Sprintf("seg_d%d", i)] = segDelay(anim, (i-1)*delay)
	}
	vars["seg_d21"] = segDelay(anim, 20*delay)

	if err := core.ApplyComponentCSSTmpl(b, sel, headingCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("heading 组件样式解析失败: %v", err))
	}
}

// init 注册标题组件。
func init() {
	core.Register(Widget)
}
