package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func adminJsSource(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}

	root := filepath.Join(filepath.Dir(self), "..", "..", "..", "..")
	p := filepath.Join(root, "internal", "templates", "static", "js", "admin.js")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 admin.js 失败: %v", err)
	}
	return string(src)
}

func TestPanelPreviewClickPinsInsteadOfToggling(t *testing.T) {
	src := adminJsSource(t)

	if strings.Contains(src, "var willOpen = !root.classList.contains('is-open')") {
		t.Error("admin.js 的 click 分支退回「is-open 取反」的纯开关（F1 复发的形状）：hover 打开后点击会把它关掉")
	}

	for _, want := range []string{
		"is-pinned",
		"root.classList.contains('is-pinned')",
		"if (root.classList.contains('is-pinned')) return;",
		"setPinned(root, true);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("admin.js 缺少 %q —— click 的固定语义不完整（触屏只能打开、不能固定，或 hover 时仍会被关掉）", want)
		}
	}

	if !strings.Contains(src, "if (canHover) {") {
		t.Error("admin.js 缺少 canHover 判定 —— hover 路径必须与 click 路径显式分开")
	}
}

func TestPanelPreviewEscapePreemptsDrawer(t *testing.T) {
	src := adminJsSource(t)

	anchor := strings.Index(src, "document.querySelector('[data-panel-preview].is-open')")
	if anchor < 0 {
		t.Fatal("未找到浮层的 Esc 处理：查询「打开中的浮层」的锚点缺失")
	}
	tail := src[anchor:]
	if len(tail) > 700 {
		tail = tail[:700]
	}
	for _, want := range []string{
		"e.preventDefault();",
		"e.stopPropagation();",
		"close(open);",
		"focusTrigger(open);",
		"}, true);",
	} {
		if !strings.Contains(tail, want) {
			t.Errorf("浮层 Esc 处理缺少 %q（在锚点之后 700 字符内未找到）—— Esc 的浮层优先权失效："+
				"抽屉会先把浮层一起关掉、焦点掉到 body", want)
		}
	}
}
