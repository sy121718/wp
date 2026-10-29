// Package addtocart 实现 core.addToCart 加购组件。
// 构建期从商品数据解析变体并输出静态表单；访问时由统一写基座提交 JSON API，
// 购物车读者收到成功事件后刷新事实。多变体商品可逐变体展示，价格与库存由服务校验。
package addtocart

import (
	_ "embed" // addtocart.css / add_to_cart.jet 经 //go:embed 打进二进制
	"errors"
	"fmt"
	"github.com/google/uuid"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/builder/components/productbinding"
	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.addToCart"

// 变体呈现方式。
const (
	// ModeSingle 只输出一个加购表单：使用明确选定的规格，未指定时取第一项。
	ModeSingle = "single"
	// ModePerVariant 每个启用变体一行（带规格标签与价格），各自一个加购按钮。
	ModePerVariant = "perVariant"
)

// CartAddPath 公开访客加购 API。
const CartAddPath = "/api/site/cart/add"

// 缺省值与上限。
const (
	defaultLabel    = "加入购物车"
	defaultCurrency = "¥"
	// defaultColor 缺省主色（主题 Token + 兜底，与 badge/button 的取色约定对齐）。
	defaultColor = "var(--sky-c-primary, #2563eb)"
)

// Props 加购组件属性。
type Props struct {
	ProductSource string `json:"productSource,omitempty" ct:"select,current=当前商品,specific=指定商品,hidden,sec=content,label=商品来源"`
	ProductID     string `json:"productId,omitempty" ct:"string,maxlen=36,hidden,sec=content,label=商品"`
	VariantID     string `json:"variantId,omitempty" ct:"string,maxlen=36,hidden,sec=content,label=规格"`

	// Source 数据源实体类型（当前只有 product；跨数据源绑定会被校验拒绝）。
	Source string `json:"source,omitempty" ct:"select,product=商品,default=product,hidden,sec=content,label=数据源"`
	// OptionsField 规格维度字段（JSON 数组，如 product.options）：变体的规格标签靠它拼出来
	//（「红色 · L」），与商品详情/规格选择器同源。
	OptionsField string `json:"optionsField,omitempty" ct:"string,maxlen=60,hidden,sec=content,label=规格维度字段"`
	// VariantsField 变体组合字段（JSON 数组，如 product.variants）：变体 id 与价格从它取。
	VariantsField string `json:"variantsField,omitempty" ct:"string,maxlen=60,hidden,sec=content,label=变体组合字段"`
	// Currency 仅控制价格展示前缀，不改变结算币种。
	Currency string `json:"currency,omitempty" ct:"string,maxlen=8,sec=content,label=价格展示符号"`
	// VariantMode 变体呈现：single（单个规格）/ perVariant（逐规格一行）。
	VariantMode string `json:"variantMode,omitempty" ct:"select,single=单个变体,perVariant=逐变体,default=single,sec=content,label=变体呈现"`
	// ShowQuantity 是否显示数量输入（关掉就是「一键加购一件」）。
	ShowQuantity bool `json:"showQuantity,omitempty" ct:"bool,sec=content,label=数量输入"`
	// Label 按钮文字。
	Label string `json:"label,omitempty" ct:"string,maxlen=20,sec=content,label=按钮文字"`
	// Color 按钮主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=按钮颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// addtocartLegacyKeys 旧 props 名的归一表（改名不迁移数据）。
//
// buttonText → label：这一批把三个组件的按钮/链接文案统一成 label。
// 存量文档里存的是旧键（本仓实测：blueprints 2 张、content_templates 1 张、
// document_snapshots 5.78 万条），不归一的表现是**按钮整颗消失**而不是报错：
// BuildView 里 HasButton / 有无按钮 依赖 label 非空。
var addtocartLegacyKeys = []core.LegacyKey{
	{From: "buttonText", To: "label"},
}

// NormalizeLegacy 见 core.LegacyNormalizer：把旧键的值挪进已解码的字段。
func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
	return core.ApplyLegacyKeysInto(raw, p, addtocartLegacyKeys)
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "加购按钮",
		Hint:            "加入购物车（价格与库存由服务校验）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"source":        "product",
			"optionsField":  "product.options",
			"variantsField": "product.variants",
			"variantMode":   "single",
			"showQuantity":  false,
			"label":         "加入购物车",
			"currency":      "¥",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// 只翻「作者填写的文本」：按钮文字是给访客看的文案，理应按构建语言翻译。
		Translatable: []string{"label"},
	},
}

// validateExtra 先拒绝非法配置，再把未选商品的编辑态交给预览局部占位。
func validateExtra(p *Props, nodeID string) error {
	bindingErr := productbinding.Validate(p.ProductSource, p.ProductID)
	if bindingErr != nil && !errors.Is(bindingErr, core.ErrIncompleteNode) {
		return bindingErr
	}
	if p.Source != "" && p.Source != "product" {
		return fmt.Errorf("无效的商品数据源")
	}
	if p.OptionsField != "" && p.OptionsField != "product.options" {
		return fmt.Errorf("无效的商品规格字段")
	}
	if p.VariantsField != "" && p.VariantsField != "product.variants" {
		return fmt.Errorf("无效的商品变体字段")
	}
	if p.VariantID != "" && uuid.Validate(p.VariantID) != nil {
		return fmt.Errorf("无效的商品规格引用")
	}
	return bindingErr
}

// effectiveMode 有效变体呈现方式（未知值兜底 single）。
func effectiveMode(p *Props) string {
	if p.VariantMode == ModePerVariant {
		return ModePerVariant
	}
	return ModeSingle
}

// effectiveLabel 有效按钮文字。
func effectiveLabel(p *Props) string {
	if t := strings.TrimSpace(p.Label); t != "" {
		return t
	}
	return defaultLabel
}

// effectiveCurrency 有效货币符号。
func effectiveCurrency(p *Props) string {
	if c := strings.TrimSpace(p.Currency); c != "" {
		return c
	}
	return defaultCurrency
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

// compileCSS 加购表单样式（逐变体模式额外的一组规则由规则级 @if 控制）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"color":      effectiveColor(p),
		"perVariant": "",
	}
	if effectiveMode(p) == ModePerVariant {
		vars["perVariant"] = "1"
	}
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
