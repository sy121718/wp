package productselector

import (
	_ "embed"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/builder/core"
	"go_wp/internal/builder/productbinding"
)

const Type = "core.productSelector"

const (
	defaultOptionsField  = "product.options"
	defaultVariantsField = "product.variants"
	defaultCurrency      = "¥"

	StockFallback = "以结算时库存为准"
)

type Props struct {
	ProductSource string `json:"productSource,omitempty" ct:"select,current=当前商品,specific=指定商品,hidden,sec=content,label=商品来源"`
	ProductID     string `json:"productId,omitempty" ct:"string,maxlen=36,hidden,sec=content,label=商品"`

	OptionsField  string             `json:"optionsField,omitempty" ct:"string,maxlen=60,hidden,sec=content,label=规格维度字段"`
	VariantsField string             `json:"variantsField,omitempty" ct:"string,maxlen=60,hidden,sec=content,label=规格组合字段"`
	Currency      string             `json:"currency,omitempty" ct:"string,maxlen=8,sec=content,label=价格展示符号"`
	Stock         string             `json:"stock,omitempty" ct:"select,=开启,off=关闭,default=,sec=content,label=实时可用量"`
	EmptyText     string             `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`
	Advanced      core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct {
	core.Atom[Props]
}

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:        Type,
		ValidateExtra:   validateExtra,
		PaletteCategory: core.PaletteCategoryBasic,
		DisplayName:     "规格选择器",
		Hint:            "选规格切组合（可放详情页任意位置）",
		DefaultProps: map[string]any{
			"optionsField":  "product.options",
			"variantsField": "product.variants",
			"currency":      "¥",
			"emptyText":     "该商品暂无可选规格",
		},
	},
}}

func validateExtra(p *Props, _ string) (err error) {
	bindingErr := productbinding.Validate(p.ProductSource, p.ProductID)
	if bindingErr != nil && !errors.Is(bindingErr, core.ErrIncompleteNode) {
		return bindingErr
	}
	for _, field := range []string{EffectiveOptionsField(p), EffectiveVariantsField(p)} {
		parts := strings.SplitN(field, ".", 2)
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("无效的字段路径 %q（期望 数据源.字段名，如 product.options）", field)
		}
		if parts[0] != "product" {
			return fmt.Errorf("字段 %q 不属于数据源 product（规格选择器只吃商品数据）", field)
		}
	}
	switch p.Stock {
	case "", "off":
	default:
		return fmt.Errorf("无效的实时可用量开关 %q（空 = 开启 / off = 关闭）", p.Stock)
	}
	return bindingErr
}

func (c *Component) FieldBindings(node *core.Node) (refs []core.FieldRef, err error) {
	if node == nil {
		return nil, nil
	}
	var p Props
	if len(node.Props) > 0 {
		if uerr := json.Unmarshal(node.Props, &p); uerr != nil {
			return nil, fmt.Errorf("props 反序列化失败: %w", uerr)
		}
	}
	for _, field := range []string{EffectiveOptionsField(&p), EffectiveVariantsField(&p)} {
		parts := strings.SplitN(field, ".", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("无效的字段路径 %q", field)
		}
		refs = append(refs, core.FieldRef{EntityType: parts[0], Field: parts[1]})
	}
	return refs, nil
}

func EffectiveOptionsField(p *Props) string {
	if p == nil || strings.TrimSpace(p.OptionsField) == "" {
		return defaultOptionsField
	}
	return strings.TrimSpace(p.OptionsField)
}

func EffectiveVariantsField(p *Props) string {
	if p == nil || strings.TrimSpace(p.VariantsField) == "" {
		return defaultVariantsField
	}
	return strings.TrimSpace(p.VariantsField)
}

func EffectiveCurrency(p *Props) string {
	if p == nil || strings.TrimSpace(p.Currency) == "" {
		return defaultCurrency
	}
	return p.Currency
}

func ShowStock(p *Props) bool {
	return p == nil || p.Stock != "off"
}

func EffectiveEmptyText(p *Props) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.EmptyText)
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

//go:embed productselector.css
var productselectorCSS string

func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productselectorCSS); err != nil {

		panic(fmt.Sprintf("productselector 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("product_selector", productselectorTemplate)
}

//go:embed product_selector.jet
var productselectorTemplate string
