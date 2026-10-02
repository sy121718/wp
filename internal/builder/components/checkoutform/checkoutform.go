// Package checkoutform 实现 core.checkoutForm：结算表单组件（含构建期数据注入）。
//
// 设计边界（改之前先读这三条，它们决定了本组件的形状）：
//
//  1. **字段清单的唯一真源是订单契约**（ordercontract.CheckoutFormFields），不是作者
//     自由添加的输入框。落库约束与同源约束都指向同一个事实：字段必须有 orders 列承接，
//     而静态产物里的表单与片段端收参必须对得上 —— 契约里没有的 key，构建期直接报错，
//     **不静默跳过**（跳过会让作者在编辑器里配出一个永远不会生效的字段）。
//  2. **Props 只管怎么显示**：勾哪些字段、顺序、标签文案、占位提示、按钮文字、是否收集
//     账单地址。国家下拉的选项是站点级构建输入（core.RenderContext.Checkout），
//     与 core.languages 的链接清单同性质 —— 组件只决定怎么显示。
//  3. **信任边界**：金额与归属类字段永不进字段库（运费 / 合计 / userId 一个都没有）。
//     见 runtimefragment/cart.go 的「运费刻意不从表单取」。
//
// 访问面硬约束：**零客户端 JS**。表单是原生 <form> + hx-post（htmx 缺席时原生 POST
// 照旧可用），国家是原生 <select>（选项构建期烘焙）。
//
// 本批不做省市区级联：只有中国有区划数据，级联要么上 JS、要么把上千个选项烘进产物，
// 两条路都不是本批的范围 —— 省市区保持文本输入。
package checkoutform

import (
	_ "embed" // checkoutform.css / checkoutform.jet 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

// Type 组件类型标识。
const Type = "core.checkoutForm"

// 数量与长度上限（防注入与防滥用）。
const (
	// maxFields 字段数量上限：订单契约的可选字段一共 20 个，留一点余量。
	maxFields = 32
	// maxLabelLen 标签字符数上限。
	maxLabelLen = 60
	// maxPlaceholderLen 占位提示字符数上限。
	maxPlaceholderLen = 120
	// maxSubmitLabelLen 提交按钮文字上限。
	maxSubmitLabelLen = 40
)

// Field 作者勾选的一个字段（**只能从订单字段清单里挑**，见 Props.Fields）。
//
// Label / Placeholder / Required 是**显示层覆盖**：Label 为空时用清单里的默认标签；
// Required 只能把非必填字段**加严**成必填 —— 清单里必填的字段无法被在这里放松，
// 因为服务端照旧会拒（见 validateExtra）。
type Field struct {
	// Key 字段标识：必须是 ordercontract 清单里的 key。
	Key string `json:"key"`
	// Label 标签文案覆盖（空 = 用清单默认标签）。
	Label string `json:"label,omitempty" ct:"text,maxlen=60,sec=content,label=标签"`
	// Required 必填覆盖（只能加严，不能放松清单必填项）。
	Required bool `json:"required,omitempty" ct:"bool,sec=content,label=必填"`
	// Placeholder 占位提示（空 = 不输出 placeholder 属性）。
	Placeholder string `json:"placeholder,omitempty" ct:"text,maxlen=120,sec=content,label=占位提示"`
}

// Props 结算表单属性。
//
// 说明：字段清单（有哪些字段可用）与国家下拉选项**都不在这里配置** ——
// 前者是订单契约的静态声明，后者是站点级构建输入（sys_area），由装配层在构建期注入。
type Props struct {
	// Fields 勾选的字段与顺序（数组字段无 ct tag，validateExtra 手写校验）。
	//
	// 为空 = 用清单里「默认勾选」的那一套（保证插入即合法）；非空时**清单里必填的
	// 字段必须都在**，否则校验失败 —— 关掉必填字段等于让每一次提交必然被服务端拒。
	Fields []Field `json:"fields,omitempty"`
	// SubmitLabel 提交按钮文字（空 = 内置默认「提交订单」）。
	SubmitLabel string `json:"submitLabel,omitempty" ct:"text,maxlen=40,sec=content,label=提交按钮文字"`
	// CollectBilling 是否收集账单地址（关时整组不渲染，账单缺省与收货地址相同）。
	CollectBilling bool `json:"collectBilling,omitempty" ct:"bool,sec=content,label=收集账单地址"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "结算表单",
		Hint:            "购物车结算：字段来自订单契约，标签与顺序可配",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			// 默认字段 = 清单里 DefaultOn 的那一套（key 由 Go 侧算，见 DefaultFieldProps）。
			"fields":         DefaultFieldProps(),
			"submitLabel":    DefaultSubmitLabel,
			"collectBilling": false,
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（docs/06-D §7.5 决策 F6）：字段标签与占位提示
		// 是**作者填的内容**，按内容翻译走；按钮文字同。键 / 必填 / 顺序一律不翻译。
		Translatable: []string{"submitLabel", "label", "placeholder"},
	},
}

// DefaultFieldProps 组件库插入时的默认字段列表（清单里 DefaultOn 的字段，只带 key）。
//
// 只给 key 不给标签：标签留空才能让「清单默认标签」这条路走得通 ——
// 若这里把中文标签一起写进默认 Props，日后再改清单文案会被这些快照盖住。
func DefaultFieldProps() []any {
	fields := ordercontract.CheckoutFormFields()
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		if f.DefaultOn {
			out = append(out, map[string]any{"key": f.Key})
		}
	}
	return out
}

// validateExtra 关系性校验：key 必须来自订单字段清单、不得重复、清单必填项不可缺失。
func validateExtra(p *Props, nodeID string) (err error) {
	if len([]rune(p.SubmitLabel)) > maxSubmitLabelLen {
		return fmt.Errorf("提交按钮文字过长（上限 %d 字符）", maxSubmitLabelLen)
	}
	if len(p.Fields) > maxFields {
		return fmt.Errorf("字段数量超出上限 %d", maxFields)
	}
	_, err = resolveSelection(p)
	return err
}

// resolveSelection 把 Props.Fields 解析成清单字段列表（顺序即渲染顺序）。
//
// 三条规则，都在这里落地（BuildView 与校验共用同一份实现，避免「校验过了、渲染另一套」）：
//
//	· 空 Fields = 清单里 DefaultOn 的字段；
//	· 每个 key 必须 ∈ 清单，否则报错（不静默跳过）；
//	· 清单必填的字段必须全部在列（关掉 = 每次提交必然被服务端拒）。
func resolveSelection(p *Props) (selected []ordercontract.CheckoutField, err error) {
	catalog := ordercontract.CheckoutFormFields()
	if len(p.Fields) == 0 {
		for _, f := range catalog {
			if f.DefaultOn {
				selected = append(selected, f)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("订单字段清单为空（结算链路没有声明任何可收集字段）")
		}
		return selected, nil
	}

	seen := make(map[string]bool, len(p.Fields))
	selected = make([]ordercontract.CheckoutField, 0, len(p.Fields))
	for i, sel := range p.Fields {
		key := strings.TrimSpace(sel.Key)
		f, ok := ordercontract.CheckoutFieldOf(key)
		if !ok || !f.InForm() {
			return nil, fmt.Errorf("第 %d 个字段 %q 不在订单字段清单里（结算表单只能收集订单契约声明的字段）", i+1, key)
		}
		if seen[key] {
			return nil, fmt.Errorf("第 %d 个字段 %q 重复", i+1, key)
		}
		seen[key] = true
		if len([]rune(sel.Label)) > maxLabelLen {
			return nil, fmt.Errorf("字段 %q 的标签过长（上限 %d 字符）", key, maxLabelLen)
		}
		if len([]rune(sel.Placeholder)) > maxPlaceholderLen {
			return nil, fmt.Errorf("字段 %q 的占位提示过长（上限 %d 字符）", key, maxPlaceholderLen)
		}
		selected = append(selected, f)
	}
	for _, f := range catalog {
		if f.Required && !seen[f.Key] {
			return nil, fmt.Errorf("字段 %q 是结算必填项，不能从表单移除", f.Key)
		}
	}
	return selected, nil
}

// checkoutformCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed checkoutform.css
var checkoutformCSS string

// compileCSS 生成结算表单样式（纯静态，交互只有 hover / active / 原生校验态）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, checkoutformCSS); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("checkoutform 组件样式解析失败: %v", err))
	}
}

// checkoutformTemplate 组件模板。与 .go / .css 同目录：改结构不必去
// internal/templates/components/ 找（注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed checkoutform.jet
var checkoutformTemplate string

// init 注册结算表单组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("checkoutform", checkoutformTemplate)
}
