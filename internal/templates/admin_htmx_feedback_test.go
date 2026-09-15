package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// admin_htmx_feedback_test.go — 审计 UI-011：后台 HTMX 请求必须有统一的 loading 指示与失败提示。
//
// 判据分三层，缺任何一层都会退化回「点了没反应 / 失败了也静默」：
//  ① 外壳里有全局指示元素与主题化样式（页面不需要逐个写 hx-indicator）；
//  ② admin.js 在 document.body 上注册请求生命周期与三类失败监听（挂在具体元素上的
//     监听覆盖不到全部 hx- 请求，而全后台的 hx- 请求是零散分布的）；
//  ③ 所监听的事件名确实是 htmx 运行时会派发的 —— 名字写错不会报错，只会永远不触发。

// adminShellData 渲染后台外壳（layout + 页面）所需的最小数据。
// "t" 必须注入：后台模板已全面 t() 化，缺它渲染会在第一处取词处静默截断。
func adminShellData() map[string]any {
	return map[string]any{
		"title": "仪表盘", "lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"csrf_token": "tok", "HasSubnav": true, "SidebarPinned": true,
		"t": TranslateFunc("zh-CN"),
	}
}

// newAdminTestSet 建一个与运行期同构的模板集合（相对 admin/ 之外的目录，扩展名 .html）。
func newAdminTestSet() *jet.Set {
	return jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
}

// 外壳必须带上全局进度条：元素 + 样式 + 三段失败文案（JS 兜底文案的 i18n 来源）。
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

// admin.js 必须在 document.body 上注册全局监听，且失败路径真的走到 error toast。
func TestAdminJSGlobalHtmxFeedbackHandlers(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/admin.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)

	for _, ev := range []string{
		"htmx:beforeRequest", "htmx:afterRequest",
		"htmx:responseError", "htmx:sendError", "htmx:timeout",
	} {
		if !strings.Contains(js, "document.body.addEventListener('"+ev+"'") {
			t.Errorf("admin.js 未在 document.body 上注册 %s —— 覆盖不到全部 hx- 请求", ev)
		}
	}

	// htmx 默认只有 2xx 会替换目标节点，4xx/5xx 的响应被直接丢弃：
	// 不弹提示就是「保存失败但界面毫无变化」，用户会认为已经保存成功。
	if !strings.Contains(js, "WBUI.toast(") || !strings.Contains(js, "type: 'error'") {
		t.Error("admin.js 的失败路径没有走到 error toast（失败会静默）")
	}

	// loading 与收尾必须成对：忙碌态 + 进度条显隐各有一条控制路径。
	if !strings.Contains(js, "is-busy") || !strings.Contains(js, "setBar(") {
		t.Error("admin.js 缺少忙碌态或进度条显隐控制")
	}

	// 触发元素在响应时已被移出文档的情况下，htmx 会对最近的存活祖先补派发一次
	// afterRequest（同一个 xhr）；不按 xhr 去重会多减引用计数，进度条提前消失。
	if !strings.Contains(js, "WeakSet") {
		t.Error("admin.js 未对 afterRequest 做同一 xhr 去重")
	}
}

// 监听的事件名必须真的存在于 htmx 运行时：写错的事件名不会报错，只会永远不触发。
func TestHtmxRuntimeDispatchesEventNamesWeListenFor(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/ui/htmx.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, ev := range []string{
		"htmx:beforeRequest", "htmx:afterRequest",
		"htmx:responseError", "htmx:sendError", "htmx:timeout",
	} {
		if !strings.Contains(js, ev) {
			t.Errorf("htmx 运行时里没有 %s —— admin.js 的监听永远不会触发", ev)
		}
	}
}
