package userforms

import "go_wp/internal/builder/core"

// View 只包含构建期表单配置和前端文案，不包含访客凭据。
type ModeTitle struct {
	Mode  string
	Title string
}

type View struct {
	Titles      []ModeTitle
	Mode        string
	Title       string
	ShowTitle   bool
	Next        string
	Labels      Labels
	ScriptHint  string
	RetryText   string
	TitleKeys   map[string]string
	customTitle bool
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) { compileCSS(id, p, b) }
func BuildView(p *Props, _, _ string) View {
	v := View{Mode: effectiveMode(p), Title: effectiveTitle(p), ShowTitle: p.ShowTitle, Next: p.Next, customTitle: p.Title != "" && p.Title != formSpecs[effectiveMode(p)].title, TitleKeys: map[string]string{}}
	v.ApplyI18n(nil)
	return v
}
func (v View) DeclareFeatures() (attrs, classes []string) {
	return []string{"data-component-instance", "data-user-form", "data-site-read", "data-site-view", "data-write"}, nil
}
