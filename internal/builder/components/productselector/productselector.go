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
	_ "embed" // productselector.css 经 //go:embed 打进二进制
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

// productselectorCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed productselector.css
var productselectorCSS string

// compileCSS 选择器样式：原生 radio + label 的胶囊值组 + 组合清单行。
//
// 选项全部是静态规则（无条件分支、无 Props 驱动取值），所以 Go 侧只交出作用域选择器，
// 声明与顺序整体放在样式源里。
func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productselectorCSS); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("productselector 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
}
