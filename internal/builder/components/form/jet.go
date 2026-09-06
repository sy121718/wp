// Package form — Jet 渲染路径辅助导出。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 form.jet 模板（<form> + 字段控件 + 提交按钮）。
// 字段值（label/name/placeholder/options）为标量/切片字段，模板输出时由 Jet 默认转义。
package form

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出表单样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// FieldView 单个字段的渲染视图（供 form.jet 模板使用）。
type FieldView struct {
	// Type 字段类型：text / email / textarea / select / checkbox。
	Type string
	// Label 字段标签（模板输出时由 Jet 默认转义）。
	Label string
	// Name 字段 name（表单提交键，已过白名单校验）。
	Name string
	// Required 是否必填（渲染 required 属性）。
	Required bool
	// Placeholder 占位提示（模板输出时由 Jet 默认转义）。
	Placeholder string
	// Options select 选项（模板输出时由 Jet 默认转义）。
	Options []string
}

// View form 渲染视图数据（供 form.jet 模板使用）。
type View struct {
	// Method 提交方式：post / get（缺省 post）。
	Method string
	// Action 提交地址（空则不输出 action 属性）。
	Action string
	// SubmitLabel 提交按钮文字（缺省「提交」）。
	SubmitLabel string
	// Fields 字段列表（与 Props.Fields 一一对应，顺序一致）。
	Fields []FieldView
}

// BuildView 生成表单渲染视图：字段列表 + 提交语义（method/submitLabel 缺省回退）。
func BuildView(p *Props) View {
	method := p.Method
	if method == "" {
		method = "post"
	}
	submitLabel := p.SubmitLabel
	if submitLabel == "" {
		submitLabel = "提交"
	}
	fields := make([]FieldView, 0, len(p.Fields))
	for _, f := range p.Fields {
		fields = append(fields, FieldView{
			Type:        f.Type,
			Label:       f.Label,
			Name:        f.Name,
			Required:    f.Required,
			Placeholder: f.Placeholder,
			Options:     f.Options,
		})
	}
	return View{
		Method:      method,
		Action:      p.Action,
		SubmitLabel: submitLabel,
		Fields:      fields,
	}
}
