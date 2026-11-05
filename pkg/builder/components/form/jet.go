package form

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type FieldView struct {
	Type string

	Label string

	Name string

	Required bool

	Placeholder string

	Options []string
}

type View struct {
	Method string

	Action string

	SubmitLabel string

	Fields []FieldView
}

const TextKeySubmit = "site.component.form.submit"

const textFallbackSubmit = "提交"

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

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if v.SubmitLabel != "" && v.SubmitLabel != textFallbackSubmit {
		return
	}
	if text == nil {
		v.SubmitLabel = textFallbackSubmit
		return
	}
	v.SubmitLabel = text(TextKeySubmit, textFallbackSubmit)
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	for _, f := range v.Fields {
		if f.Type == "select" {
			return []string{"data-ui-select"}, nil
		}
	}
	return nil, nil
}
