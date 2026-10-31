package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func adminShellData() map[string]any {
	return map[string]any{
		"title": "仪表盘", "lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"csrf_token": "tok", "HasSubnav": true, "SidebarPinned": true,
		"t": TranslateFunc("zh-CN"),
	}
}

func newAdminTestSet() *jet.Set {
	set := jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	return set
}

func TestAdminHtmxProgressIndicatorInShell(t *testing.T) {
	out, err := render(t, newAdminTestSet(), "admin/dashboard", adminShellData())
	if err != nil {
		t.Fatalf("渲染后台外壳失败: %v", err)
	}
	for _, want := range []string{
		"data-hx-progress",
		".hx-progress",
		"prefers-reduced-motion",
		"data-msg-error",
		"data-msg-network",
		"data-msg-timeout",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("后台外壳缺少 %q —— 统一 loading 指示或失败提示的载体不在页面上", want)
		}
	}
}

func TestAdminJSGlobalHtmxFeedbackHandlers(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/admin.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)

	for _, ev := range []string{

		"htmx:before:request", "htmx:after:request",
		"htmx:response:error", "htmx:error",
	} {
		if !strings.Contains(js, "document.body.addEventListener('"+ev+"'") {
			t.Errorf("admin.js 未在 document.body 上注册 %s —— 覆盖不到全部 hx- 请求", ev)
		}
	}

	if !strings.Contains(js, "WBUI.toast(") || !strings.Contains(js, "type: 'error'") {
		t.Error("admin.js 的失败路径没有走到 error toast（失败会静默）")
	}

	if !strings.Contains(js, "is-busy") || !strings.Contains(js, "setBar(") {
		t.Error("admin.js 缺少忙碌态或进度条显隐控制")
	}

	if !strings.Contains(js, "WeakSet") {
		t.Error("admin.js 未对 after:request 做同一请求去重")
	}
}

func TestHtmxRuntimeDispatchesEventNamesWeListenFor(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/ui/htmx.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, ev := range []string{

		"htmx:before:request", "htmx:after:request",
		"htmx:response:error", "htmx:error",
	} {
		if !strings.Contains(js, ev) {
			t.Errorf("htmx 运行时里没有 %s —— admin.js 的监听永远不会触发", ev)
		}
	}
}
