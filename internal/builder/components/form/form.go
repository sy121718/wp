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
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
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
	b.Add(core.BreakpointDesktop, sel+" .wp-form-field", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 6px",
	})
	// 标签样式。
	b.Add(core.BreakpointDesktop, sel+" .wp-form-field label", []string{
		"font-size: 14px",
		"font-weight: 600",
		"color: inherit",
	})
	// checkbox 标签：横向排列 + 字重回退（覆盖上方统一 label 样式）。
	b.Add(core.BreakpointDesktop, sel+" .wp-form-field label.wp-form-check", []string{
		"display: inline-flex",
		"align-items: center",
		"gap: 8px",
		"font-weight: 400",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-form-field label.wp-form-check input", []string{
		"width: auto",
		"margin: 0",
	})
	// 输入控件通用边框样式。
	inputDecls := []string{
		"width: 100%",
		"box-sizing: border-box",
		"padding: 10px 12px",
		"font-size: 15px",
		"border: 1px solid rgba(0,0,0,.15)",
		"border-radius: 6px",
		"background: #fff",
		"color: #111",
		"transition: border-color .15s",
	}
	b.Add(core.BreakpointDesktop, sel+" input[type=text]", inputDecls)
	b.Add(core.BreakpointDesktop, sel+" input[type=email]", inputDecls)
	b.Add(core.BreakpointDesktop, sel+" textarea", append(append([]string{}, inputDecls...), "min-height: 96px", "resize: vertical"))
	b.Add(core.BreakpointDesktop, sel+" select", inputDecls)
	// 聚焦边框高亮。
	focusDecls := []string{"border-color: var(--c-primary, #2563eb)", "outline: none"}
	b.Add(core.BreakpointDesktop, sel+" input:focus", focusDecls)
	b.Add(core.BreakpointDesktop, sel+" textarea:focus", focusDecls)
	b.Add(core.BreakpointDesktop, sel+" select:focus", focusDecls)
	// 提交按钮样式。
	b.Add(core.BreakpointDesktop, sel+" .wp-form-submit", []string{
		"align-self: flex-start",
		"padding: 11px 24px",
		"font-size: 15px",
		"font-weight: 600",
		"color: #fff",
		"background: var(--c-primary, #2563eb)",
		"border: none",
		"border-radius: 6px",
		"cursor: pointer",
		"transition: background .15s",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-form-submit:hover", []string{
		"background: var(--c-primary-dark, #1d4ed8)",
	})
}

// init 注册表单组件。
func init() {
	core.Register(Widget)
}
