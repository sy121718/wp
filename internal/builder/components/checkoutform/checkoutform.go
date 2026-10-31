package checkoutform

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

const Type = "core.checkoutForm"

const (
	maxFields = 32

	maxLabelLen = 60

	maxPlaceholderLen = 120

	maxSubmitLabelLen = 40
)

type Field struct {
	Key string `json:"key"`

	Label string `json:"label,omitempty" ct:"string,maxlen=60,sec=content,label=标签"`

	Required bool `json:"required,omitempty" ct:"bool,sec=content,label=必填"`

	Placeholder string `json:"placeholder,omitempty" ct:"string,maxlen=120,sec=content,label=占位提示"`
}

type Props struct {
	Fields []Field `json:"fields,omitempty"`

	SubmitLabel string `json:"submitLabel,omitempty" ct:"string,maxlen=40,sec=content,label=提交按钮文字"`

	CollectBilling bool `json:"collectBilling,omitempty" ct:"bool,sec=content,label=收集账单地址"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct{ core.Atom[Props] }

func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{
		{Key: "back", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionInstance}, TargetCapability: "commerce.cart"},
		{Key: "success", AllowedKinds: []core.ActionKind{core.ActionLocal, core.ActionInstance}, TargetCapability: "commerce.checkout.result"},
	}
}
func (*Component) RequiredActions() []string { return nil }
func (*Component) Parts() []core.PartSpec {
	return []core.PartSpec{{Key: "checkout", Capabilities: []string{"commerce.checkout"}}, {Key: "result", Capabilities: []string{"commerce.checkout.result"}}}
}

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "结算表单",
		Hint:            "购物车结算：字段来自订单契约，标签与顺序可配",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{

			"fields":         DefaultFieldProps(),
			"submitLabel":    DefaultSubmitLabel,
			"collectBilling": false,
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"submitLabel", "label", "placeholder"},
	},
}}

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

//go:embed checkoutform.css
var checkoutformCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, checkoutformCSS); err != nil {

		panic(fmt.Sprintf("checkoutform 组件样式解析失败: %v", err))
	}
}

//go:embed checkoutform.jet
var checkoutformTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("checkoutform", checkoutformTemplate)
}
