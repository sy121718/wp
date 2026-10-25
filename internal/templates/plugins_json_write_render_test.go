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
func TestPluginsJSONWriteExternalForms(t *testing.T) {
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
			for _, tc := range []struct{ id, api, mode string }{{"plugin-toggle-hello", "/api/plugin/toggle", "create"}, {"plugin-uninstall-hello", "/api/plugin/uninstall", "single"}} {
				f := forms[tc.id]
				if f == nil {
					t.Fatalf("missing form %s", tc.id)
				}
				for k, want := range map[string]string{"data-write-url": tc.api, "action": tc.api, "data-write": tc.mode} {
					got, _ := pluginAttr(f, k)
					if got != want {
						t.Errorf("%s %s=%q want %q", tc.id, k, got, want)
					}
				}
				// data-write-host 已删：新基座统一用 closest('[data-write]') 找宿主。
				// 下面三条仍要守：宿主不能隐藏结论区（否则失败提示看不见）。
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
			// data-write-bool 已删：实测 formdata 的 on=1 直接绑进 bool，
			// 未勾选时浏览器根本不提交该键（服务端得到 false）。守复选框 name 仍在。
			hasEnabled := false
			for c := f.FirstChild; c != nil; c = c.NextSibling {
				if v, ok := pluginAttr(c, "name"); ok && v == "enabled" {
					hasEnabled = true
				}
			}
			if !hasEnabled {
				t.Fatal("toggle 表单缺少 name=enabled 的复选框 —— 服务端收不到开关状态")
			}
			if v, _ := pluginAttr(f, "data-write-nav"); v != "/admin/plugins" {
				t.Fatal("toggle navigation missing")
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
			if v, _ := pluginAttr(forms["plugin-uninstall-hello"], "data-confirm"); v == "" {
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
