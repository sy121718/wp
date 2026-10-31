package form

import (
	_ "embed"
	"fmt"
	"regexp"

	"go_wp/internal/builder/core"
)

const Type = "core.form"

const (
	FieldText     = "text"
	FieldEmail    = "email"
	FieldTextarea = "textarea"
	FieldSelect   = "select"
	FieldCheckbox = "checkbox"
)

const (
	maxFields      = 20
	maxLabelLen    = 100
	maxPlaceholder = 200
	maxOptions     = 20
	maxOptionLen   = 100
)

var fieldNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,49}$`)

var fieldTypes = map[string]bool{
	FieldText:     true,
	FieldEmail:    true,
	FieldTextarea: true,
	FieldSelect:   true,
	FieldCheckbox: true,
}

type FormField struct {
	Type string `json:"type"`

	Label string `json:"label"`

	Name string `json:"name"`

	Required bool `json:"required,omitempty"`

	Placeholder string `json:"placeholder,omitempty"`

	Options []string `json:"options,omitempty"`
}

type Props struct {
	Fields []FormField `json:"fields,omitempty"`

	SubmitLabel string `json:"submitLabel,omitempty" ct:"text,maxlen=50,sec=content,label=提交按钮文字"`

	Action string `json:"action,omitempty" ct:"text,maxlen=500,sec=content,label=提交地址"`

	Method string `json:"method,omitempty" ct:"select,post=POST,get=GET,default=post,sec=content,label=提交方式"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "表单",
		Hint:            "联系/订阅表单",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"fields": []any{
				map[string]any{
					"type":     "text",
					"label":    "姓名",
					"name":     "name",
					"required": true,
				},
				map[string]any{
					"type":     "email",
					"label":    "邮箱",
					"name":     "email",
					"required": true,
				},
			},
			"submitLabel": "提交",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"submitLabel", "label", "placeholder", "options"},
	},
}

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

//go:embed form.css
var formCSS string

//go:embed form.jet
var formTemplate string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, formCSS); err != nil {

		panic(fmt.Sprintf("form 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("form", formTemplate)
}
