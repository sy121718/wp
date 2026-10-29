// Package addtocart 实现 core.addToCart 加购组件。
// 构建期从商品数据解析变体并输出静态表单；访问时由统一写基座提交 JSON API，
// 仅使用当前商品上下文，买家在自己的表单里选规格；价格与库存由服务校验。
package addtocart

import (
	_ "embed" // addtocart.css / add_to_cart.jet 经 //go:embed 打进二进制
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.addToCart"

// CartAddPath 公开访客加购 API。
const CartAddPath = "/api/site/cart/add"

// 缺省值与上限。
const (
	defaultLabel = "加入购物车"
	// defaultColor 缺省主色（主题 Token + 兜底，与 badge/button 的取色约定对齐）。
	defaultColor = "var(--sky-c-primary, #171717)"
)

// Props 加购组件属性。
type Props struct {
	// ShowQuantity 是否显示数量输入（关掉就是「一键加购一件」）。
	ShowQuantity bool `json:"showQuantity,omitempty" ct:"bool,sec=content,label=数量输入"`
	// Label 按钮文字。
	Label string `json:"label,omitempty" ct:"string,maxlen=20,sec=content,label=按钮文字"`
	// Color 按钮主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=按钮颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// addtocartLegacyKeys 仅保留作者按钮文案的旧命名归一。
var addtocartLegacyKeys = []core.LegacyKey{
	{From: "buttonText", To: "label"},
}

// NormalizeLegacy 见 core.LegacyNormalizer：把旧键的值挪进已解码的字段。
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

// Component 固定加购语义，不接受作者自定义流程目标。
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

// Widget 基座实例。
var Widget = Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "加购按钮",
		Hint:            "加入购物车（价格与库存由服务校验）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps:    map[string]any{"showQuantity": false, "label": "加入购物车"},
		TypeName:        Type,
		// 只翻「作者填写的文本」：按钮文字是给访客看的文案，理应按构建语言翻译。
		Translatable: []string{"label"},
	},
}}

// effectiveLabel 有效按钮文字。
func effectiveLabel(p *Props) string {
	if t := strings.TrimSpace(p.Label); t != "" {
		return t
	}
	return defaultLabel
}

// effectiveColor 有效按钮颜色（空则主题 Token 兜底）。
func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return defaultColor
}

//go:embed addtocart.css
var addToCartCSS string

// compileCSS 加购表单的实例样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"color": effectiveColor(p)}
	if err := core.ApplyComponentCSSTmpl(b, sel, addToCartCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷：静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("addToCart 组件样式解析失败: %v", err))
	}
	// 交互反馈走 bucket API 而不是样式源：hover 的规则必须包在 @media (hover: hover) 里
	//（触屏上根本不输出，否则「鼠标划过变亮」在手机上等于不存在），而样式源只支持
	// max-width 断点。按压反馈用 AddActive —— 它不带媒体查询，触屏上照样有反馈。
	// UI-006 判定：按钮的悬停提亮**不补触屏等价形态** —— 触屏没有悬停，等价反馈
	// 就是下面那条 @active 按压（手指按下即触发），加一层常驻态反而成了假状态。
	// 注意：这两个 API 只负责「包 @media」和「进哪个桶」，**不替你加伪类** ——
	// 选择器要自带 :hover / :active。漏掉的后果不是「没有交互反馈」而是「反馈恒定生效」：
	// 提亮规则会一直亮着、按压规则会把按钮一直压暗，而且不报任何错。
	b.AddHover(sel+" .sky-cart-add-btn:hover", []string{"filter: brightness(1.08)"})
	b.AddActive(sel+" .sky-cart-add-btn:active", []string{"filter: brightness(0.94)"})
	// 购物车列表样式由基座按 data-site-cart 注入，按钮组件只负责自身外观。
}

//go:embed add_to_cart.jet
var addToCartTemplate string

// init 注册加购组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("add_to_cart", addToCartTemplate)
}
