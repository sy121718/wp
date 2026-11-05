package card

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/pkg/builder/core"
)

const Type = "core.card"

type Props struct {
	Title string `json:"title,omitempty" ct:"text,maxlen=200,sec=content,label=标题"`

	TitleTag string `json:"titleTag,omitempty" ct:"select,=默认（h3）,h2=二级标题,h3=三级标题,h4=四级标题,h5=五级标题,default=,sec=content,label=标题层级"`

	Text string `json:"text,omitempty" ct:"richtext,maxlen=1000,sec=content,label=正文"`

	ImageSrc string `json:"imageSrc,omitempty" ct:"media,maxlen=500,sec=content,label=图片"`

	Label string `json:"label,omitempty" ct:"text,maxlen=50,sec=content,label=按钮文字"`

	ButtonLink string `json:"buttonLink,omitempty" ct:"text,maxlen=500,sec=content,label=按钮链接"`

	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`

	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var cardLegacyKeys = []core.LegacyKey{
	{From: "buttonText", To: "label"},
}

func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
	return core.ApplyLegacyKeysInto(raw, p, cardLegacyKeys)
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "卡片",
		Hint:            "标题+正文+按钮",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"title":      "卡片标题",
			"text":       "卡片正文内容。",
			"label":      "了解更多",
			"buttonLink": "/",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"title", "text", "label"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.ButtonLink != "" && !core.IsSafeURL(p.ButtonLink) {
		return fmt.Errorf("按钮链接协议非法: %q", p.ButtonLink)
	}
	return nil
}

//go:embed card.css
var cardCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, cardCSS); err != nil {

		panic(fmt.Sprintf("card 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("card", cardTemplate)
}

//go:embed card.jet
var cardTemplate string
