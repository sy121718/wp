package slider

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"

	"go_wp/internal/builder/core"
)

//go:embed enhance.js
var enhanceJS string

const Type = "core.slider"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("slider", sliderTemplate)
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initSliders"},
		Feats:  []string{"data-slider"},
		Source: enhanceJS,
	})
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"perView": map[string]any{
				"desktop": 1,
			},
			"autoplay":   0,
			"showArrows": true,
			"showDots":   true,
			"gap":        "16px",
		},
		DisplayName: "轮播",
		Hint:        "多屏滑动（可嵌套）",
	}
}

type PerView struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

type Props struct {
	PerView PerView `json:"perView,omitempty"`

	Autoplay float64 `json:"autoplay,omitempty" ct:"number,sec=content,label=自动播放间隔(s)"`

	ShowArrows bool `json:"showArrows,omitempty" ct:"bool,sec=content,label=显示箭头"`

	ShowDots bool `json:"showDots,omitempty" ct:"bool,sec=content,label=显示圆点"`

	Loop bool `json:"loop,omitempty" ct:"bool,sec=content,label=循环播放"`

	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=content,label=间距"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.ValidateSelf(node, ids); err != nil {
		return err
	}
	return core.ValidateChildren(node, ids)
}

func (c *Component) ValidateSelf(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.PerView.Desktop < 0 || p.PerView.Desktop > 4 || p.PerView.Tablet < 0 || p.PerView.Tablet > 4 || p.PerView.Mobile < 0 || p.PerView.Mobile > 4 {
		return fmt.Errorf("节点 %s: 每屏显示数需在 0~4 之间", node.ID)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		if err = core.ValidateAdvanced(adv, node.ID, ids); err != nil {
			return err
		}
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("%w: 节点 %s: 轮播至少需要一个 slide（把组件拖入内部）", core.ErrIncompleteNode, node.ID)
	}
	return nil
}

//go:embed slider.css
var sliderCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	gap := p.Gap
	if gap == "" {
		gap = "16px"
	}

	perView := p.PerView.Desktop
	if perView <= 0 {
		perView = 1
	}
	if perView > 4 {
		perView = 4
	}
	slideW := func(n int) string {
		if n <= 0 || n > 4 {
			return ""
		}
		return strconv.FormatFloat(100.0/float64(n), 'f', 4, 64) + "%"
	}

	vars := map[string]string{
		"gap":          gap,
		"slideDesktop": slideW(perView),
		"slideTablet":  slideW(p.PerView.Tablet),
		"slideMobile":  slideW(p.PerView.Mobile),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, sliderCSS, vars); err != nil {
		panic(fmt.Sprintf("slider 组件样式解析失败: %v", err))
	}
}

//go:embed slider.jet
var sliderTemplate string
