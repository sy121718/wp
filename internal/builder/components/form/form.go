// Package form 实现 core.form：表单组件（对标 GrapesJS 的 forms 插件生态）。
//
// 原子组件：由 Fields 列表声明的受控表单字段（text/email/textarea/select/checkbox），
// 编译期输出原生 <form> + 受控控件，method/action 提交语义与提交按钮文案由 Props 声明。
// 字段值（name/label/placeholder/options）全部经过白名单与长度校验，输出层由 Jet 默认
// 转义，双层防注入。零客户端 JS：原生 HTML 表单，提交行为交给 method/action 声明。
package form

import (
	"fmt"
	"regexp"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.form"

// 字段类型白名单。
const (
	FieldText     = "text"
	FieldEmail    = "email"
	FieldTextarea = "textarea"
	FieldSelect   = "select"
	FieldCheckbox = "checkbox"
)

// 数量与长度上限（防注入与防滥用）。
const (
	maxFields      = 20  // 字段数量上限
	maxLabelLen    = 100 // 标签字符数上限
	maxPlaceholder = 200 // 占位提示字符数上限
	maxOptions     = 20  // select 选项数量上限
	maxOptionLen   = 100 // 单个选项字符数上限
)

// fieldNameRe 表单键白名单：字母开头，后接字母/数字/下划线，总长 1~50。
var fieldNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,49}$`)

// fieldTypes 字段类型白名单集合。
var fieldTypes = map[string]bool{
	FieldText:     true,
	FieldEmail:    true,
	FieldTextarea: true,
	FieldSelect:   true,
	FieldCheckbox: true,
}

// FormField 单个表单字段。
type FormField struct {
	// Type 字段类型：text / email / textarea / select / checkbox。
	Type string `json:"type"`
	// Label 字段标签（用户可见文本，渲染时由 Jet 转义）。
	Label string `json:"label"`
	// Name 字段 name（表单提交键，白名单校验）。
	Name string `json:"name"`
	// Required 是否必填。
	Required bool `json:"required,omitempty"`
	// Placeholder 占位提示（text/email/textarea 用）。
	Placeholder string `json:"placeholder,omitempty"`
	// Options select 的选项。
	Options []string `json:"options,omitempty"`
}

// Props form 属性。
type Props struct {
	// Fields 表单字段列表（数组字段无 ct tag，validateExtra 手写校验）。
	Fields []FormField `json:"fields,omitempty"`
	// SubmitLabel 提交按钮文字。
	SubmitLabel string `json:"submitLabel,omitempty" ct:"text,maxlen=50,sec=content,label=提交按钮文字"`
	// Action 提交地址。
	Action string `json:"action,omitempty" ct:"text,maxlen=500,sec=content,label=提交地址"`
	// Method 提交方式：post / get（默认 post）。
	Method string `json:"method,omitempty" ct:"select,post=POST,get=GET,default=post,sec=content,label=提交方式"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"submitLabel", "label", "placeholder", "options"},
	},
}

// validateExtra 关系性校验：字段白名单、表单键白名单、select 选项约束与文本长度防注入。
func validateExtra(p *Props, nodeID string) (err error) {
	if len(p.Fields) == 0 {
		return fmt.Errorf("表单至少需要一个字段")
	}
	if len(p.Fields) > maxFields {
		return fmt.Errorf("表单字段数量超出上限 %d", maxFields)
	}
	for i, f := range p.Fields {
		if !fieldTypes[f.Type] {
			return fmt.Errorf("第 %d 个字段类型非法: %q（仅 text/email/textarea/select/checkbox）", i+1, f.Type)
		}
		if f.Label == "" {
			return fmt.Errorf("第 %d 个字段缺少标签", i+1)
		}
		if len([]rune(f.Label)) > maxLabelLen {
			return fmt.Errorf("第 %d 个字段标签过长（上限 %d 字符）", i+1, maxLabelLen)
		}
		if f.Name == "" {
			return fmt.Errorf("第 %d 个字段缺少 name", i+1)
		}
		if !fieldNameRe.MatchString(f.Name) {
			return fmt.Errorf("第 %d 个字段 name 非法: %q（需字母开头，仅字母/数字/下划线，长度 1~50）", i+1, f.Name)
		}
		if len([]rune(f.Placeholder)) > maxPlaceholder {
			return fmt.Errorf("第 %d 个字段占位提示过长（上限 %d 字符）", i+1, maxPlaceholder)
		}
		if f.Type == FieldSelect {
			if len(f.Options) == 0 {
				return fmt.Errorf("第 %d 个 select 字段必须提供选项", i+1)
			}
			if len(f.Options) > maxOptions {
				return fmt.Errorf("第 %d 个 select 字段选项数量超出上限 %d", i+1, maxOptions)
			}
			for j, opt := range f.Options {
				if len([]rune(opt)) > maxOptionLen {
					return fmt.Errorf("第 %d 个字段第 %d 个选项过长（上限 %d 字符）", i+1, j+1, maxOptionLen)
				}
			}
		}
	}
	if p.Action != "" && !core.IsSafeURL(p.Action) {
		return fmt.Errorf("无效的提交地址: %q", p.Action)
	}
	return nil
}

// compileCSS 表单样式：字段纵向排列、label 样式、input/textarea/select 边框、submit button 样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 表单容器：纵向布局 + 字段间距。
	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex",
		"flex-direction: column",
		"gap: 16px",
	})
	// 单个字段：纵向排列。
	b.Add(core.BreakpointDesktop, sel+" .sky-form-field", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 6px",
	})
	// 标签样式。
	b.Add(core.BreakpointDesktop, sel+" .sky-form-field label", []string{
		"font-size: 14px",
		"font-weight: 600",
		"color: inherit",
	})
	// checkbox 标签：横向排列 + 字重回退（覆盖上方统一 label 样式）。
	b.Add(core.BreakpointDesktop, sel+" .sky-form-field label.sky-form-check", []string{
		"display: inline-flex",
		"align-items: center",
		"gap: 8px",
		"font-weight: 400",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-form-field label.sky-form-check input", []string{
		"width: auto",
		"margin: 0",
	})
	// 输入控件通用边框样式。
	inputDecls := []string{
		"width: 100%",
		"box-sizing: border-box",
		"padding: 10px 12px",
		"font-size: 15px",
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,.15))",
		"border-radius: 6px",
		// 输入框跟主题表面色/正文色走（此前写死白底黑字，主题改了输入框也不变）。
		"background: var(--sky-c-surface, #fff)",
		"color: var(--sky-c-text, #1f2430)",
		core.FocusTransitionDecl(),
	}
	// :is() 合并同声明选择器（规则数 4→2，产物体积更小；:is 特异性取参数最高者，
	// 与拆分写法一致，不改变覆盖行为）。
	b.Add(core.BreakpointDesktop, sel+" :is(input[type=text],input[type=email],select)", inputDecls)
	b.Add(core.BreakpointDesktop, sel+" textarea", append(append([]string{}, inputDecls...), "min-height: 96px", "resize: vertical"))
	// 聚焦边框高亮 + 光晕 ring（效果基本库 core.FocusRingDecls，--sky-focus-ring 可主题覆写）。
	focusDecls := core.FocusRingDecls()
	b.Add(core.BreakpointDesktop, sel+" :is(input,textarea,select):focus", focusDecls)
	// 校验错误态（:has() 父选择器 + 原生 :user-invalid，零 JS）：
	// 用户交互后字段非法 → 字段容器与输入框同步标红，无需 JS 遍历 DOM。
	// 用 :user-invalid 而非 :invalid：避开「刚打开页面就全部标红」的体验问题。
	b.Add(core.BreakpointDesktop, sel+" .sky-form-field:has(:user-invalid)", []string{
		"color: var(--sky-danger, #dc2626)",
	})
	b.Add(core.BreakpointDesktop, sel+" :user-invalid", []string{
		"border-color: var(--sky-danger, #dc2626)",
		"box-shadow: 0 0 0 3px rgba(220, 38, 38, .12)",
	})
	// 提交按钮样式。
	b.Add(core.BreakpointDesktop, sel+" .sky-form-submit", []string{
		"align-self: flex-start",
		"padding: 11px 24px",
		"font-size: 15px",
		"font-weight: 600",
		"color: var(--sky-btn-color, #fff)",
		"background: var(--sky-btn-bg, var(--sky-c-primary, #2563eb))",
		"border: none",
		"border-radius: 6px",
		"cursor: pointer",
		"transition: background .15s",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-form-submit:hover", []string{
		"background: var(--sky-btn-hover-bg, var(--sky-c-primary, #1d4ed8))",
	})
}

// init 注册表单组件。
func init() {
	core.Register(Widget)
}
