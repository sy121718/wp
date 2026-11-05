package addtocart

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.addToCart"

const CartAddPath = "/api/site/cart/add"

const (
	defaultLabel = "加入购物车"

	defaultColor = "var(--sky-c-primary, #171717)"
)

type Props struct {
	ShowQuantity bool `json:"showQuantity,omitempty" ct:"bool,sec=content,label=数量输入"`

	Label string `json:"label,omitempty" ct:"string,maxlen=20,sec=content,label=按钮文字"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=按钮颜色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var addtocartLegacyKeys = []core.LegacyKey{
	{From: "buttonText", To: "label"},
}

func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"productSource", "productId", "variantId", "source", "optionsField", "variantsField", "currency", "variantMode", "action", "target", "cartTarget"} {
		if _, exists := fields[key]; exists {
			return fmt.Errorf("旧加购接线已停用，请在内容面板点击“改为当前商品加购”（字段 %s）", key)
		}
	}
	return core.ApplyLegacyKeysInto(raw, p, addtocartLegacyKeys)
}

type Component struct{ core.Atom[Props] }

func (c Component) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.Atom.Validate(node, ids); err != nil {
		return err
	}
	if len(node.Actions) > 0 {
		return fmt.Errorf("加购动作固定，请在内容面板点击“改为当前商品加购”清理旧动作")
	}
	return nil
}

var Widget = Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "加购按钮",
		Hint:            "加入购物车（价格与库存由服务校验）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps:    map[string]any{"showQuantity": false, "label": "加入购物车"},
		TypeName:        Type,

		Translatable: []string{"label"},
	},
}}

func effectiveLabel(p *Props) string {
	if t := strings.TrimSpace(p.Label); t != "" {
		return t
	}
	return defaultLabel
}

func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return defaultColor
}

//go:embed addtocart.css
var addToCartCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"color": effectiveColor(p)}
	if err := core.ApplyComponentCSSTmpl(b, sel, addToCartCSS, vars); err != nil {

		panic(fmt.Sprintf("addToCart 组件样式解析失败: %v", err))
	}

	b.AddHover(sel+" .sky-cart-add-btn:hover", []string{"filter: brightness(1.08)"})
	b.AddActive(sel+" .sky-cart-add-btn:active", []string{"filter: brightness(0.94)"})

}

//go:embed add_to_cart.jet
var addToCartTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("add_to_cart", addToCartTemplate)
}
