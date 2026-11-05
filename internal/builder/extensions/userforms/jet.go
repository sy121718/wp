package userforms

import "go_wp/pkg/builder/core"

type ModeTitle struct {
	Mode  string
	Title string
}

type PreviewLabel struct{ State, Text string }

type View struct {
	Titles                 []ModeTitle
	Mode                   string
	Title                  string
	Description            string
	AutoTitle              bool
	ShowTitle              bool
	Next                   string
	Labels                 Labels
	ScriptHint             string
	RetryText              string
	TitleKeys              map[string]string
	PreviewLabels          []PreviewLabel
	AuthenticatedAction    core.ResolvedAction
	HasAuthenticatedAction bool
	customTitle            bool
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) { compileCSS(id, p, b) }
func BuildView(p *Props, _, _ string) View {
	customTitle := p.Title != "" && p.Title != formSpecs[effectiveMode(p)].title
	v := View{Mode: effectiveMode(p), Title: effectiveTitle(p), Description: p.Description, AutoTitle: !customTitle, ShowTitle: p.ShowTitle, Next: p.Next, customTitle: customTitle, TitleKeys: map[string]string{}}
	v.ApplyI18n(nil)
	return v
}
func (v View) DeclareFeatures() (attrs, classes []string) {
	attrs = []string{"data-component-instance", "data-component-part", "data-user-form", "data-site-read", "data-site-view", "data-write"}
	if v.HasAuthenticatedAction {
		attrs = append(attrs, "data-component-action")
	}
	return attrs, nil
}

func (v *View) ResolveActions(node *core.Node, ctx *core.RenderContext) error {
	if _, ok := node.Actions["authenticated"]; !ok {
		return nil
	}
	action, err := ctx.ResolveAction(node, "authenticated")
	if err != nil {
		return err
	}
	v.AuthenticatedAction, v.HasAuthenticatedAction = action, true
	return nil
}
