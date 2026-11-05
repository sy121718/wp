package templates

import (
	"os"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func TestAdminSettingsTemplateParses(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	if _, err := set.GetTemplate("admin/project/settings"); err != nil {
		t.Fatalf("站点设置页模板解析失败: %v", err)
	}
}

func TestAdminSettingsHasNotFoundHTMLEditor(t *testing.T) {
	src, err := os.ReadFile("admin/project/settings.html")
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	body := string(src)
	for _, want := range []string{
		`name="notFoundHtml"`,
		`{{.NotFoundHTML}}`,
		`maxlength="32768"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("站点设置页缺少 %q", want)
		}
	}
}

func TestAdminSettingsRendersNotFoundHTML(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	const page = "<!doctype html><h1>页面不存在</h1>"
	data := settingsRenderData([]map[string]any{{"ID": "p1", "Name": "站点"}}, "p1")
	data["NotFoundHTML"] = page
	html, err := render(t, set, "admin/project/settings", data)
	if err != nil {
		t.Fatalf("站点设置页渲染失败: %v", err)
	}
	if !strings.Contains(html, "页面不存在") {
		t.Error("已配置的自定义 404 页内容未回显到表单")
	}
}

func settingsRenderData(projects []map[string]any, selected string) map[string]any {
	return map[string]any{
		"lang": "zh-CN", "title": "站点设置", "menu": "settings", "t": TranslateFunc("zh-CN"),
		"csrf_token": "tok",
		"Projects":   projects,
		"Selected":   selected,
		"Name":       "站点",

		"SiteName":                  "站点显示名",
		"SiteDesc":                  "",
		"ContactEmail":              "",
		"IndexNowKey":               "",
		"GA4MeasurementID":          "",
		"SearchConsoleVerification": "",
		"NotFoundHTML":              "",
		"URLPatterns":               []map[string]any{},

		"ShippingBaseFeeYuan":       "",
		"ShippingFreeThresholdYuan": "",
		"Locales":                   []map[string]any{},

		"LocaleError":       "",
		"LangURLOffWarning": false,
	}
}

func TestAdminSettingsHeaderProjectActions(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	for _, tc := range []struct {
		name     string
		projects []map[string]any
		selected string
		actions  bool
	}{
		{name: "无工程", projects: []map[string]any{}},
		{name: "单工程", projects: []map[string]any{{"ID": "p1", "Name": "站点"}}, selected: "p1"},
		{name: "多工程", projects: []map[string]any{{"ID": "p1", "Name": "站点甲"}, {"ID": "p2", "Name": "站点乙"}}, selected: "p2", actions: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := render(t, set, "admin/project/settings", settingsRenderData(tc.projects, tc.selected))
			if err != nil {
				t.Fatalf("站点设置页渲染失败: %v", err)
			}
			if !strings.Contains(out, "</html>") {
				t.Fatal("站点设置页未完整渲染")
			}
			if tc.actions {
				headStart := strings.Index(out, `<header class="page-head">`)
				if headStart < 0 {
					t.Fatal("多工程页缺少页头")
				}
				headEnd := strings.Index(out[headStart:], "</header>")
				if headEnd < 0 {
					t.Fatal("多工程页头未闭合")
				}
				head := out[headStart : headStart+headEnd]
				for _, want := range []string{`class="page-actions"`, `action="/admin/settings"`, `name="project"`, `value="p1"`, `value="p2" selected`} {
					if !strings.Contains(head, want) {
						t.Errorf("页头的多工程切换缺少 %q", want)
					}
				}
				if got := strings.Count(head, "<option "); got != 2 {
					t.Errorf("切换器应列出两个工程，实际 %d 个", got)
				}
			} else if strings.Contains(out, `class="page-actions"`) || strings.Contains(out, `id="settings-project"`) {
				t.Error("没有可切换工程时仍渲染了空操作区或工程选择器")
			}
		})
	}
}
