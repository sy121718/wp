// Package addtocart 实现 core.addToCart 加购组件。
//
// 为什么需要它：购物车与结算的片段能力已经落地，但静态产物里没有任何地方能发起加购 ——
// 页面作者得手写一段带 hidden variantId 的表单，还得自己知道片段端点、幂等键与降级怎么写。
// 本组件把那件事变成一次拖拽：变体 id 在**构建期**从商品数据解析并烘进产物，
// 与规格选择器同源（同一个 product.variants 字段、同一份解析函数），
// 所以「详情页显示的规格组合」与「能加购的变体」不会各说各话。
//
// 两条路径同一个 action：
//
//	· 有 HTMX —— hx-post 局部刷新购物车容器（默认 #cart）；
//	· 无 JS —— 原生表单 POST 到同一端点，浏览器整页跳到片段 HTML（可用但朴素）。
//
// 为什么不做「选规格再加购」的联动：那需要 JS 把 radio 的选中值塞进表单，而规格选择器
// 目前是纯原生 radio（不驱动任何别的元素）。强行加联动会让「无 JS 时按钮提交一个错的变体」——
// 那种错比「多变体商品在卡片上只提供一键加购」严重得多。多变体场景请用逐变体模式，
// 或在详情页把规格选择器与逐变体列表放在一起。
package addtocart

import (
	_ "embed" // addtocart.css / add_to_cart.jet 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.addToCart"

// 变体呈现方式。
const (
	// ModeSingle 只输出一个加购表单：取商品启用变体里价格最低的那个。
	//
	// 用在卡片上（一键加购）。多变体商品取最低价是**展示口径**而不是「默认变体」——
	// 商品数据里没有「默认变体」这个概念，凭空造一个会与规格选择器的首选项打架。
	ModeSingle = "single"
	// ModePerVariant 每个启用变体一行（带规格标签与价格），各自一个加购按钮。
	ModePerVariant = "perVariant"
)

// CartAddPath 加购片段端点（片段层的固定路径，与 runtimefragment 的注册一致）。
const CartAddPath = "/_fragments/cartAdd"

// 缺省值与上限。
const (
	defaultButtonText = "加入购物车"
	defaultCartTarget = "#cart"
	defaultCurrency   = "¥"
	// defaultColor 缺省主色（主题 Token + 兜底，与 badge/button 的取色约定对齐）。
	defaultColor = "var(--sky-c-primary, #2563eb)"
)

// Props 加购组件属性。
type Props struct {
	// Source 数据源实体类型（当前只有 product；跨数据源绑定会被校验拒绝）。
	Source string `json:"source,omitempty" ct:"select,product=商品,default=product,sec=content,label=数据源"`
	// OptionsField 规格维度字段（JSON 数组，如 product.options）：变体的规格标签靠它拼出来
	//（「红色 · L」），与商品详情/规格选择器同源。
	OptionsField string `json:"optionsField,omitempty" ct:"string,maxlen=60,sec=content,label=规格维度字段"`
	// VariantsField 变体组合字段（JSON 数组，如 product.variants）：变体 id 与价格从它取。
	VariantsField string `json:"variantsField,omitempty" ct:"string,maxlen=60,sec=content,label=变体组合字段"`
	// Currency 货币符号（价格前缀；留空用默认符号）。
	Currency string `json:"currency,omitempty" ct:"text,maxlen=8,sec=content,label=货币符号"`
	// VariantMode 变体呈现：single（一键加购最低价变体）/ perVariant（逐变体一行）。
	VariantMode string `json:"variantMode,omitempty" ct:"select,single=单个变体,perVariant=逐变体,default=single,sec=content,label=变体呈现"`
	// ShowQuantity 是否显示数量输入（关掉就是「一键加购一件」）。
	ShowQuantity bool `json:"showQuantity,omitempty" ct:"bool,sec=content,label=数量输入"`
	// ButtonText 按钮文字。
	ButtonText string `json:"buttonText,omitempty" ct:"text,maxlen=20,sec=content,label=按钮文字"`
	// CartTarget 加购后要刷新的购物车容器选择器（HTMX 用；无 JS 时忽略）。
	CartTarget string `json:"cartTarget,omitempty" ct:"text,maxlen=60,sec=style,label=购物车容器选择器"`
	// Color 按钮主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=按钮颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "加购按钮",
		Hint:            "加入购物车（提交到购物车片段）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"source":        "product",
			"optionsField":  "product.options",
			"variantsField": "product.variants",
			"variantMode":   "single",
			"showQuantity":  false,
			"buttonText":    "加入购物车",
			"cartTarget":    "#cart",
			"currency":      "¥",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// 只翻「作者填写的文本」：按钮文字是给访客看的文案，理应按构建语言翻译。
		Translatable: []string{"buttonText"},
	},
}

// validateExtra 关系性校验：没有变体来源的加购按钮点了只会报错，在保存期就拦住。
func validateExtra(p *Props, nodeID string) (err error) {
	if strings.TrimSpace(p.OptionsField) == "" {
		// 没有规格维度就拼不出变体标签，逐变体模式会渲染成一排没有区别的按钮 ——
		// 「L 和 XL 长一样」这种事只能靠保存期拦住。
		return fmt.Errorf("加购组件必须声明规格维度字段（如 product.options）")
	}
	if strings.TrimSpace(p.VariantsField) == "" {
		return fmt.Errorf("加购组件必须声明变体组合字段（如 product.variants）")
	}
	return nil
}

// effectiveMode 有效变体呈现方式（未知值兜底 single）。
func effectiveMode(p *Props) string {
	if p.VariantMode == ModePerVariant {
		return ModePerVariant
	}
	return ModeSingle
}

// effectiveButtonText 有效按钮文字。
func effectiveButtonText(p *Props) string {
	if t := strings.TrimSpace(p.ButtonText); t != "" {
		return t
	}
	return defaultButtonText
}

// effectiveTarget 有效购物车容器选择器。
//
// 空白值与「看起来不像选择器」的值都兜底：一个错的选择器会让 HTMX 静默不刷新
// （控制台一条警告，页面上什么都不会发生），比回退到默认值难查得多。
func effectiveTarget(p *Props) string {
	t := strings.TrimSpace(p.CartTarget)
	if t == "" || !looksLikeSelector(t) {
		return defaultCartTarget
	}
	return t
}

// looksLikeSelector 粗判一个字符串像不像 CSS 选择器（#id / .class / [attr] / 字母开头）。
func looksLikeSelector(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '#', '.', '[':
		return len(s) > 1
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
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
	// 购物车**片段**的样式不在这里注入（审计 UIK-005）：片段样式已归基座
	// （internal/builder/fragment_base.go），按 hx-* 指向 /_fragments/ 的特征判定。
}

//go:embed add_to_cart.jet
var addToCartTemplate string

// init 注册加购组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("add_to_cart", addToCartTemplate)
}
