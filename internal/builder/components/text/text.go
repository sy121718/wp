package text

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.text"

const (
	ModeRichText  = "richtext"
	ModePlainText = "plaintext"
)

const (
	maxPlainLen = 2000
	maxExcerpt  = 400
	maxClamp    = 10
)

type Binding struct {
	Field    string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"`
	Fallback string `json:"fallback,omitempty"`
}

type Props struct {
	Mode string `json:"mode,omitempty" ct:"select,richtext=富文本,plaintext=纯文本,default=richtext,sec=content,label=内容模式"`

	PlainTag string `json:"plainTag,omitempty" ct:"select,p=段落,span=行内,default=p,sec=content,label=包裹标签"`

	Text string `json:"text,omitempty" ct:"richtext,maxlen=30000,sec=content"`

	Binding *Binding `json:"binding,omitempty"`

	Typography core.TextStyle `json:"typography,omitempty"`

	ParagraphSpacing string `json:"paragraphSpacing,omitempty" ct:"dimension,maxlen=30,sec=style"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`

	LinkColor string `json:"linkColor,omitempty" ct:"color,maxlen=200,sec=style"`

	LineClamp int `json:"lineClamp,omitempty" ct:"slider,min=0,max=10,step=1,sec=style"`

	Excerpt int `json:"excerpt,omitempty" ct:"slider,min=0,max=400,step=5,sec=style"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

		Translatable: []string{"text"},
	},
}

var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

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
	if p.Excerpt > 0 && p.Mode != ModeRichText {
		return fmt.Errorf("摘要模式仅限富文本绑定场景")
	}
	if err = core.ValidateTextStyle(nodeID, &p.Typography); err != nil {
		return err
	}
	if strings.TrimSpace(p.Text) == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("%w: 必须提供内容或 CMS 绑定", core.ErrIncompleteNode)
	}
	return nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

//go:embed text.css
var textCSS string

func typoDecls(p *Props, bp string) string {
	return strings.Join(p.Typography.BreakpointDecls(bp), "; ")
}

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

		panic(fmt.Sprintf("text 组件样式解析失败: %v", err))
	}

	if isRichText(p) {
		if err := core.ApplyComponentCSSTmpl(b, sel, textRichCSS, nil); err != nil {
			panic(fmt.Sprintf("text 富文本样式解析失败: %v", err))
		}
	}
}

func effectiveParagraphSpacing(p *Props) string {
	if p != nil {
		if v := strings.TrimSpace(p.ParagraphSpacing); v != "" {
			return v
		}
	}

	if !isRichText(p) {
		return ""
	}
	return defaultParagraphSpacing
}

func isRichText(p *Props) bool {
	if p == nil {
		return false
	}
	if p.Mode == ModePlainText {
		return false
	}
	return strings.TrimSpace(p.Text) != "" || p.Binding != nil
}

const defaultParagraphSpacing = "1em"

func init() {
	core.Register(Widget)
	core.RegisterTemplate("text", textTemplate)
}

//go:embed text_richtext.css
var textRichCSS string

//go:embed text.jet
var textTemplate string
