package quote

import (
	_ "embed"
	"fmt"

	"go_wp/internal/builder/core"
)

const Type = "core.quote"

const (
	AlignLeft   = "left"
	AlignCenter = "center"
)

type Props struct {
	Text string `json:"text,omitempty" ct:"richtext,maxlen=1000,sec=content,label=引用内容"`

	Author string `json:"author,omitempty" ct:"text,maxlen=100,sec=content,label=作者"`

	Source string `json:"source,omitempty" ct:"text,maxlen=500,sec=content,label=出处链接"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,sec=style,label=对齐"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "引用",
		Hint:            "引用块",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"text":   "引用一段有力量的话。",
			"author": "作者名",
			"align":  "left",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"text", "author"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" {
		return fmt.Errorf("必须提供引用内容")
	}
	if p.Source != "" && !core.IsSafeURL(p.Source) {
		return fmt.Errorf("出处链接协议非法: %q", p.Source)
	}
	return nil
}

//go:embed quote.css
var quoteCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"center": core.BoolVar(p.Align == AlignCenter)}
	if err := core.ApplyComponentCSSTmpl(b, sel, quoteCSS, vars); err != nil {

		panic(fmt.Sprintf("quote 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("quote", quoteTemplate)
}

//go:embed quote.jet
var quoteTemplate string
