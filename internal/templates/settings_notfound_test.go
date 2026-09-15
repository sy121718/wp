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
	if _, err := set.GetTemplate("admin/settings"); err != nil {
		t.Fatalf("站点设置页模板解析失败: %v", err)
	}
}

// TestAdminSettingsHasNotFoundHTMLEditor 表单里必须有 404 页输入项，键名与后端一致。
//
// 键名写错是这一页最容易犯又最难发现的错：表单照样渲染、保存照样 200，
// 只是那个字段永远存不进去（本页的保存是「按字段逐个读表单」）。
func TestAdminSettingsHasNotFoundHTMLEditor(t *testing.T) {
	src, err := os.ReadFile("admin/settings.html")
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
	data := map[string]any{
		"lang": "zh-CN", "title": "站点设置", "menu": "settings", "t": TranslateFunc("zh-CN"),
		"csrf_token": "tok",
		"Projects":   []map[string]any{{"ID": "p1", "Name": "站点"}},
		"Selected":   "p1",
		"Name":       "站点",
		// 表单其余字段一并给出：map 数据下缺键是**渲染错误**（不是零值），
		// 只给本用例关心的键，断言会停在无关的那一行。
		"SiteName":                  "站点显示名",
		"SiteDesc":                  "",
		"ContactEmail":              "",
		"IndexNowKey":               "",
		"GA4MeasurementID":          "",
		"SearchConsoleVerification": "",
		"NotFoundHTML":              page,
		"URLPatterns":               []map[string]any{},
		"Locales":                   []map[string]any{},
		// 三个可选键按模板既有写法直接参与 {{if}}，缺失会让渲染在那一行中断。
		"LocaleError":       "",
		"LocaleSaved":       false,
		"LangURLOffWarning": false,
	}
	html, err := render(t, set, "admin/settings", data)
	if err != nil {
		t.Fatalf("站点设置页渲染失败: %v", err)
	}
	if !strings.Contains(html, "页面不存在") {
		t.Error("已配置的自定义 404 页内容未回显到表单")
	}
	if !strings.Contains(html, `value="p1"`) {
		t.Error("工程选择器未渲染（渲染在中途中断的典型现象）")
	}
}
