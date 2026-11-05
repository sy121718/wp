package templates

import (
	"golang.org/x/net/html"
	"strings"
	"testing"
)

func pluginAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}
func TestPluginsHTMLWriteExternalForms(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "disable", false: "enable"}[enabled], func(t *testing.T) {
			data := pluginsPatrolData(patrolView{})
			data["Plugins"].([]map[string]any)[0]["Enabled"] = enabled
			out, err := render(t, pluginsPatrolSet(t), "admin/plugin/plugins", data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "</html>") || !strings.Contains(out, installFormAnchor) {
				t.Fatal("incomplete page or changed install")
			}
			doc, err := html.Parse(strings.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			forms := map[string]*html.Node{}
			buttons := map[string]*html.Node{}
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				if n.Type == html.ElementNode {
					if n.Data == "form" {
						id, _ := pluginAttr(n, "id")
						forms[id] = n
					}
					if n.Data == "button" {
						id, _ := pluginAttr(n, "form")
						buttons[id] = n
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(doc)
			for _, tc := range []struct{ id, api string }{{"plugin-toggle-hello", "/api/plugin/toggle"}, {"plugin-uninstall-hello", "/api/plugin/uninstall"}} {
				f := forms[tc.id]
				if f == nil {
					t.Fatalf("missing form %s", tc.id)
				}
				if got, _ := pluginAttr(f, "hx-post"); got != tc.api {
					t.Errorf("%s hx-post=%q want %q", tc.id, got, tc.api)
				}
				if _, ok := pluginAttr(f, "data-write"); !ok {
					t.Errorf("%s 缺 data-write 认领标记", tc.id)
				}
				for _, gone := range []string{"data-write-url", "action", "method"} {
					if got, ok := pluginAttr(f, gone); ok && gone != "method" {
						t.Errorf("%s 仍残留 %s=%q", tc.id, gone, got)
					}
				}

				host := f.Parent
				if _, ok := pluginAttr(host, "hidden"); ok {
					t.Fatal("host hides status")
				}
				cls, _ := pluginAttr(host, "class")
				if strings.Contains(cls, "hidden") {
					t.Fatal("host hidden by class")
				}
				status := false
				for c := host.FirstChild; c != nil; c = c.NextSibling {
					if _, ok := pluginAttr(c, "data-write-status"); ok {
						status = true
					}
				}
				if !status {
					t.Fatal("host lacks sibling status")
				}
				for p := f.Parent; p != nil; p = p.Parent {
					if p.Data == "table" || p.Data == "td" || p.Data == "form" {
						t.Fatal("external form nested in table or form")
					}
				}
				b := buttons[tc.id]
				if b == nil {
					t.Fatal("external submit button missing")
				}
				for _, k := range []string{"data-requires-js", "hidden", "disabled"} {
					if _, ok := pluginAttr(b, k); !ok {
						t.Errorf("button missing %s", k)
					}
				}
			}
			f := forms["plugin-toggle-hello"]

			hasEnabled := false
			for c := f.FirstChild; c != nil; c = c.NextSibling {
				if v, ok := pluginAttr(c, "name"); ok && v == "enabled" {
					hasEnabled = true
				}
			}
			if !hasEnabled {
				t.Fatal("toggle 表单缺少 name=enabled 的复选框 —— 服务端收不到开关状态")
			}
			// 成功刷新由模板声明；不再用 URL 插值的导航属性。
			if _, ok := pluginAttr(f, "data-write-reload"); !ok {
				t.Fatal("toggle 成功动作缺 data-write-reload")
			}
			if _, ok := pluginAttr(f, "data-write-nav"); ok {
				t.Fatal("toggle 不应再用 URL 导航属性")
			}
			gotEnabled := ""
			for c := f.FirstChild; c != nil; c = c.NextSibling {
				if name, _ := pluginAttr(c, "name"); name == "enabled" {
					gotEnabled, _ = pluginAttr(c, "value")
				}
			}
			want := map[bool]string{true: "false", false: "true"}[enabled]
			if gotEnabled != want {
				t.Fatalf("enabled=%q want %q", gotEnabled, want)
			}
			if v, _ := pluginAttr(forms["plugin-uninstall-hello"], "hx-confirm"); v == "" {
				t.Fatal("uninstall lacks confirmation")
			}
			if strings.Contains(out, `action="/admin/plugins/toggle"`) || strings.Contains(out, `action="/admin/plugins/uninstall"`) {
				t.Fatal("old page actions remain")
			}
			if !strings.Contains(out, "插件启停和卸载需要 JavaScript") {
				t.Fatal("precise noscript message missing")
			}
		})
	}
}
