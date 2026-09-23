package templates

// settings_notfound_test.go — 站点设置页的自定义 404 页输入项（审计 SEO-013）。
//
// 只验证模板这一层：语法能整份解析、字段真的在页面上、已配置的内容能回显。
// 「发布真的写出 404.html」与「访问面真的返回它」由 pipeline / routers 的测试覆盖。

import (
	"os"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// TestAdminSettingsTemplateParses 站点设置页必须能整份解析。
//
// 为什么要真解析：本页 extends layout.html，且表单里 Jet 表达式与 HTML 属性混排
// （value="{{...}}"、placeholder="{{ .["t"](...) }}"）。Jet 的解析错误活到运行时的
// 表现是**HTTP 200 但表单整块消失** —— 从「某一行数据没显示出来」几乎定位不到
// 模板中间那一行，所以把「能解析」变成一条可执行断言。
func TestAdminSettingsTemplateParses(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	if _, err := set.GetTemplate("admin/project/settings"); err != nil {
		t.Fatalf("站点设置页模板解析失败: %v", err)
	}
}

// TestAdminSettingsHasNotFoundHTMLEditor 表单里必须有 404 页输入项，键名与后端一致。
//
// 键名写错是这一页最容易犯又最难发现的错：表单照样渲染、保存照样 200，
// 只是那个字段永远存不进去（本页的保存是「按字段逐个读表单」）。
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

// TestAdminSettingsRendersNotFoundHTML 真渲染：已配置的 404 内容必须出现在 textarea 里。
//
// 回显断了的表现是「打开设置页看到空白，以为没配过」—— 管理员会重贴一遍内容，
// 或者更糟：以为配置丢了。
func TestAdminSettingsRendersNotFoundHTML(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
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
		// 表单其余字段一并给出：map 数据下缺键是**渲染错误**（不是零值），
		// 只给本用例关心的键，断言会停在无关的那一行。
		"SiteName":                  "站点显示名",
		"SiteDesc":                  "",
		"ContactEmail":              "",
		"IndexNowKey":               "",
		"GA4MeasurementID":          "",
		"SearchConsoleVerification": "",
		"NotFoundHTML":              "",
		"URLPatterns":               []map[string]any{},
		"Locales":                   []map[string]any{},
		// 三个可选键按模板既有写法直接参与 {{if}}，缺失会让渲染在那一行中断。
		"LocaleError":       "",
		"LocaleSaved":       false,
		"LangURLOffWarning": false,
	}
}

func TestAdminSettingsHeaderProjectActions(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
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
