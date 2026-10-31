package heading

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.heading"

const maxTextLen = 500

type DecorProps struct {
	Decoration string `json:"decoration,omitempty" ct:"select,none=无,underline=下划线,line-through=删除线,label=文字装饰"`

	DecorationColor string `json:"decorationColor,omitempty" ct:"color,maxlen=100"`
}

type Binding struct {
	Field string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"`

	Fallback string `json:"fallback,omitempty" ct:"text,maxlen=500"`
}

type Props struct {
	Text string `json:"text,omitempty" ct:"text,maxlen=500,sec=content"`

	Binding *Binding `json:"binding,omitempty"`

	Tag string `json:"tag,omitempty" ct:"select,h1=一级标题,h2=二级标题,h3=三级标题,h4=四级标题,h5=五级标题,h6=六级标题,div=区块,span=行内,default=h2,sec=content,label=语义标签"`

	Typography core.TextStyle `json:"typography,omitempty"`

	Weight string `json:"weight,omitempty" ct:"string,maxlen=10,sec=style"`

	LetterSpacing string `json:"letterSpacing,omitempty" ct:"dimension,maxlen=20,sec=style"`

	Transform string `json:"transform,omitempty" ct:"select,none=无,uppercase=全大写,lowercase=全小写,capitalize=首字母大写,sec=style,label=大小写转换"`

	Decor DecorProps `json:"decor,omitempty"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`

	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=6,step=1,sec=style"`

	TextShadow string `json:"textShadow,omitempty" ct:"select,subtle=轻阴影,strong=重阴影,sec=style,label=文字阴影"`

	TextStroke string `json:"textStroke,omitempty" ct:"select,thin=细描边,bold=粗描边,sec=style,label=文字描边"`

	Subtitle string `json:"subtitle,omitempty" ct:"text,maxlen=200,sec=content,label=副标题"`

	SubtitleColor string `json:"subtitleColor,omitempty" ct:"color,maxlen=200,sec=style,label=副标题颜色"`

	SubtitleFontSize string `json:"subtitleFontSize,omitempty" ct:"dimension,maxlen=30,sec=style,label=副标题字号"`

	SubtitleFontWeight string `json:"subtitleFontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,default=600,sec=style,label=副标题字重"`

	SubtitleSpacing string `json:"subtitleSpacing,omitempty" ct:"dimension,maxlen=30,sec=style,label=副标题间距"`

	Align Align `json:"align,omitempty" ct:"rtext,sec=layout,label=对齐"`

	Width Responsive `json:"width,omitempty" ct:"rtext,sec=layout,label=宽度"`

	HighlightColor   string `json:"highlightColor,omitempty" ct:"color,maxlen=200,sec=style,label=高亮背景色"`
	HighlightPadding string `json:"highlightPadding,omitempty" ct:"dimension,maxlen=30,sec=style,label=高亮内边距"`
	HighlightRadius  string `json:"highlightRadius,omitempty" ct:"dimension,maxlen=30,sec=style,label=高亮圆角"`

	TextAnim string `json:"textAnim,omitempty" ct:"select,=无,chars=逐字,words=逐词,sec=style,label=文本动画"`

	TextAnimDelay int `json:"textAnimDelay,omitempty" ct:"int,min=10,max=200,sec=style,label=字间延迟(ms)"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Align struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "标题",
		Hint:            "文字标题",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"text": "新标题",
			"tag":  "h2",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"text", "subtitle"},
	},
}

var (
	weightTokenMap    = map[string]int{"regular": 400, "medium": 500, "semibold": 600, "bold": 700}
	textShadowPresets = map[string]string{
		"subtle": "0 1px 2px rgba(0,0,0,0.45)",
		"strong": "0 2px 6px rgba(0,0,0,0.6)",
	}

	decorationMap = map[string]bool{"none": true, "underline": true, "line-through": true}

	fieldPathRe = regexpCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

func regexpCompile(pat string) *regexp.Regexp { return regexp.MustCompile(pat) }

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

func textAnimOn(p *Props) bool {
	return p.TextAnim != "" && p.HighlightColor == ""
}

//go:embed heading.css
var headingCSS string

func typoDecls(p *Props, bp string) string {
	return strings.Join(p.Typography.BreakpointDecls(bp), "; ")
}

func segDelay(on bool, ms int) string {
	if !on {
		return ""
	}
	return fmt.Sprintf("%dms", ms)
}

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
		"text_anim":       core.BoolVar(anim),
		"clamp":           clamp,
	}

	for i := 1; i <= 20; i++ {
		vars[fmt.Sprintf("seg_d%d", i)] = segDelay(anim, (i-1)*delay)
	}
	vars["seg_d21"] = segDelay(anim, 20*delay)

	if err := core.ApplyComponentCSSTmpl(b, sel, headingCSS, vars); err != nil {

		panic(fmt.Sprintf("heading 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("heading", headingTemplate)
}

//go:embed heading.jet
var headingTemplate string
