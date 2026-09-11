// Package productselector 实现 core.productSelector 规格选择器组件（issue #26）。
//
// 定位：把商品详情里「选规格 → 切组合」这块**拆成可独立拖拽的组件**。
// 原来的 core.product（#6/#8）把媒体区、信息区、规格选择器焊在一棵子树里，
// 想自定义详情布局就只能整块不要；本组件让作者把选择器放到任意位置。
//
// 与 core.product 的关系：共用同一份解析（ParseOptionGroups / ParseVariantOptions 由它导出），
// 也共用同一个实时可用量片段（productVariantAvailability，issue #24）—— 口径只有一份。
//
// 无 JS / HTMX 不可用时：选择器仍是**原生 radio + label**（Tab 进组、方向键切换免费拿到），
// 全部组合仍平铺可见，库存位显示构建期兜底文案 —— 信息不丢，只是不联动。
package productselector

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.productSelector"

const (
	defaultOptionsField  = "product.options"
	defaultVariantsField = "product.variants"
	defaultCurrency      = "¥"
	// StockFallback 无脚本 / 片段不可用时的兜底文案（与片段层「未知」态同一句）。
	StockFallback = "以结算时库存为准"
	// StockFragmentPath 实时可用量片段端点（issue #24）。
	StockFragmentPath = "/_fragments/productVariantAvailability"
)

// Props core.productSelector 属性。
type Props struct {
	OptionsField  string             `json:"optionsField,omitempty" ct:"string,maxlen=60,sec=content,label=规格维度字段"`
	VariantsField string             `json:"variantsField,omitempty" ct:"string,maxlen=60,sec=content,label=规格组合字段"`
	Currency      string             `json:"currency,omitempty" ct:"text,maxlen=8,sec=content,label=货币符号"`
	Stock         string             `json:"stock,omitempty" ct:"select,=开启,off=关闭,default=,sec=content,label=实时可用量"`
	EmptyText     string             `json:"emptyText,omitempty" ct:"text,maxlen=50,sec=content,label=空态文案"`
	Advanced      core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 规格选择器组件。
type Component struct {
	core.Atom[Props]
}

// Widget 组件实例。
var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{TypeName: Type, ValidateExtra: validateExtra},
}}

// validateExtra 关系性校验：字段必须来自 product 数据源（形状与数据源，不查白名单）。
//
// 字段名是否在白名单内由 builder.ValidateFieldRefs（保存期）与解析器（构建期）判定 ——
// 组件不另写一份白名单，白名单只有商品域一份。
func validateExtra(p *Props, _ string) (err error) {
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
	return nil
}

// FieldBindings 实现 core.FieldBindingProvider：自报规格维度 / 组合两个字段绑定。
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

// EffectiveOptionsField / EffectiveVariantsField / EffectiveCurrency 取值归一。
func EffectiveOptionsField(p *Props) string {
	if p == nil || strings.TrimSpace(p.OptionsField) == "" {
		return defaultOptionsField
	}
	return strings.TrimSpace(p.OptionsField)
}

// EffectiveVariantsField 规格组合字段（空取默认）。
func EffectiveVariantsField(p *Props) string {
	if p == nil || strings.TrimSpace(p.VariantsField) == "" {
		return defaultVariantsField
	}
	return strings.TrimSpace(p.VariantsField)
}

// EffectiveCurrency 货币符号（空取默认）。
func EffectiveCurrency(p *Props) string {
	if p == nil || strings.TrimSpace(p.Currency) == "" {
		return defaultCurrency
	}
	return p.Currency
}

// ShowStock 是否需要实时可用量片段（空 = 开启）。
func ShowStock(p *Props) bool {
	return p == nil || p.Stock != "off"
}

// EffectiveEmptyText 空态文案（空 = 整块不输出）。
func EffectiveEmptyText(p *Props) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.EmptyText)
}

// CompileCSS 规格选择器样式编译（jetview 经本入口调用）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// compileCSS 选择器样式：原生 radio + label 的胶囊值组 + 组合清单行。
//
// 多端硬规则：值组与组合行都折行，宽度一律 min(100%, …)；
// radio 视觉隐藏但仍可聚焦（键盘 Tab / 方向键可用，读屏读得到 label）——
// 用 position + clip-path 而不是 display:none，后者会把键盘路径一起删掉。
func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex",
		"flex-direction: column",
		"gap: 14px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-group", []string{
		"border: 0",
		"margin: 0",
		"padding: 0",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-legend", []string{
		"font-size: .9rem",
		"font-weight: 600",
		"margin-bottom: 6px",
		"padding: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-values", []string{
		"display: flex",
		"flex-wrap: wrap",
		"gap: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-radio", []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: -1px",
		"padding: 0",
		"overflow: hidden",
		"clip-path: inset(50%)",
		"white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-value", []string{
		"display: inline-flex",
		"align-items: center",
		"gap: 6px",
		"padding: 6px 14px",
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,0.15))",
		"border-radius: 999px",
		"cursor: pointer",
		"min-width: 0",
		"max-width: 100%",
	})
	// 选中态与焦点环都靠兄弟选择器（radio 在 label 之前），零 JS。
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-radio:checked + .sky-selector-value", []string{
		"border-color: var(--sky-c-primary, #2563eb)",
		"color: var(--sky-c-primary, #2563eb)",
		"background: var(--sky-c-primary-weak, rgba(37,99,235,0.08))",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-radio:focus-visible + .sky-selector-value", []string{
		"outline: 2px solid var(--sky-c-primary, #2563eb)",
		"outline-offset: 2px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-variants", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-variant", []string{
		"display: flex",
		"flex-wrap: wrap",
		"align-items: baseline",
		"gap: 10px",
		"padding: 8px 10px",
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,0.08))",
		"border-radius: 8px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-variant-price", []string{
		"font-weight: 700",
		"color: var(--sky-c-primary, #2563eb)",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-variant-compare", []string{
		"text-decoration: line-through",
		"color: var(--sky-c-muted, rgba(0,0,0,0.45))",
		"font-size: .9em",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-stock", []string{
		"font-size: .85rem",
		"color: var(--sky-c-muted, rgba(0,0,0,0.6))",
		"flex: 1 0 100%",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-variant .is-out", []string{
		"color: var(--sky-c-danger, #dc2626)",
		"font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-selector-empty", []string{
		"margin: 0",
		"color: var(--sky-c-muted, rgba(0,0,0,0.55))",
	})
}

func init() {
	core.Register(Widget)
}
