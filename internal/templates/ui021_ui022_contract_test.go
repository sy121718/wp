package templates

import (
	"os"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// UI-021/022 契约测试锁定工作台聚焦模式和独立登录页的窄屏可达性。
func TestUI021WorkbenchFocusModeContract(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	data := map[string]any{
		"title": "页面管理", "pageId": "page-1", "isBlock": false, "isTemplate": false,
		"document": "{}", "meta": "{}", "schemas": "{}", "jsVer": "test",
		// t / lang 是工作台模板的**必需键**（模板顶层 tr := .["t"] 取词，缺它文案会静默变空，
		// 与 admin 各页渲染测试同一口径）。生产路径统一由 shell.Prepare 注入。
		"t": TranslateFunc("zh-CN"), "lang": "zh-CN",
	}
	html, err := render(t, set, "workbench/layout", data)
	if err != nil {
		t.Fatalf("工作台布局渲染失败: %v", err)
	}
	for _, want := range []string{`id="wb-immersive"`, `aria-pressed="false"`, "聚焦模式 (Ctrl+P)", `id="wb-canvas"`, `id="wb-panel-library"`} {
		if !strings.Contains(html, want) {
			t.Errorf("工作台渲染缺少 %q", want)
		}
	}
	js, err := readTemplateAsset("static/js/workbench/methods/shortcuts.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"toggleImmersive()", "aria-pressed", "is-immersive", "case 'p': this.toggleImmersive()"} {
		if !strings.Contains(js, want) {
			t.Errorf("工作台聚焦模式脚本缺少 %q", want)
		}
	}
}

func TestUI022AdminLoginResponsiveContract(t *testing.T) {
	html, err := readTemplateAsset("admin/login.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"width: min(100%, 416px)", "padding: 16px", "box-sizing: border-box", "max-height: calc(100vh - 32px)", `role="alert"`, `aria-live="polite"`, "msgEl.focus()", "max-width: 42%", "outline: 2px solid var(--c-primary"} {
		if !strings.Contains(html, want) {
			t.Errorf("登录页契约缺少 %q", want)
		}
	}
}

func readTemplateAsset(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
