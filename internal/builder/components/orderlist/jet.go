package orderlist

import (
	"go_wp/internal/builder/core"
	"net/url"
	"strconv"
	"strings"
)

type LabelAttribute struct {
	Key   string
	Value string
}

type View struct {
	Title          string
	ShowTitle      bool
	ProjectID      string
	ReadURL        string
	DetailURL      string
	ReturnableURL  string
	ReturnsURL     string
	ReturnAction   string
	Notice         string
	LabelAttrs     []LabelAttribute
	Labels         map[string]string
	LoginAction    core.ResolvedAction
	HasLoginAction bool
}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) { compileCSS(id, p, b) }
func BuildView(p *Props, projectID, lang string) View {
	v := View{Title: effectiveTitle(p), ShowTitle: p.ShowTitle, ProjectID: strings.TrimSpace(projectID), ReturnAction: OrdersPath + "/returns"}
	v.ApplyI18n(nil)
	if v.ProjectID == "" {
		v.Notice = v.Labels["Unavailable"]
		return v
	}
	q := url.Values{"projectId": {v.ProjectID}, "limit": {strconv.Itoa(effectivePageSize(p))}}
	if strings.TrimSpace(lang) != "" {
		q.Set("lang", lang)
	}
	v.ReadURL = OrdersPath + "?" + q.Encode()
	q.Del("limit")
	v.DetailURL = OrdersPath + "/detail?" + q.Encode()
	v.ReturnableURL = OrdersPath + "/returnable?" + q.Encode()
	v.ReturnsURL = OrdersPath + "/returns?" + q.Encode()
	return v
}
func (v *View) ResolveActions(node *core.Node, ctx *core.RenderContext) error {
	if _, ok := node.Actions["login"]; !ok {
		return nil
	}
	action, err := ctx.ResolveAction(node, "login")
	if err != nil {
		return err
	}
	v.LoginAction = action
	v.HasLoginAction = true
	return nil
}
func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Notice != "" {
		return nil, nil
	}
	attrs = []string{"data-site-orders", "data-site-read", "data-site-view", "data-component-instance", "data-component-part", "data-write", "data-requires-js"}
	if v.HasLoginAction {
		attrs = append(attrs, "data-component-action")
	}
	return attrs, nil
}
