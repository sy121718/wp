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

// TextKeySubmit 提交按钮缺省文案的词条 key（site.component.{type}.{prop}）。
const TextKeySubmit = "site.component.form.submit"

// textFallbackSubmit 缺词条时的原中文兜底（绝不输出空串）。
const textFallbackSubmit = "提交"

// BuildView 生成表单渲染视图：字段列表 + 提交语义（method/submitLabel 缺省回退）。
func BuildView(p *Props) View {
	method := p.Method
	if method == "" {
		method = "post"
	}
	submitLabel := p.SubmitLabel
	if submitLabel == "" {
		submitLabel = textFallbackSubmit
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

// ApplyI18n 按当前语言填充提交按钮缺省文案（实现 core.I18nAware）。
//
// 用户显式填写的提交文案优先：只有「未填写」或「恰好等于内置缺省『提交』」时才翻译
// （BuildView 已把空值落为内置缺省，此处无法区分两者，故以值判定）。
// text 为 nil 或未命中词条时使用包内中文兜底，保证按钮文字永不为空。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if v.SubmitLabel != "" && v.SubmitLabel != textFallbackSubmit {
		return // 用户自定义文案不翻译
	}
	if text == nil {
		v.SubmitLabel = textFallbackSubmit
		return
	}
	v.SubmitLabel = text(TextKeySubmit, textFallbackSubmit)
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：表单里出现下拉字段时，
// form.jet 输出 data-ui-select —— 产物据此内联「原始控件基座」的下拉替身（原生 select 的
// 弹层在部分桌面环境行为异常）。没有下拉字段就一个字节都不该带。
func (v View) DeclareFeatures() (attrs, classes []string) {
	for _, f := range v.Fields {
		if f.Type == "select" {
			return []string{"data-ui-select"}, nil
		}
	}
	return nil, nil
}
