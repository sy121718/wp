package unit

// navigation_panel_preview_script_test.go — 菜单项悬浮面板预览（admin.js）两条交互缺陷的回归钉。
//
// 背景（两条交互缺陷）：
//   · F1 —— 有 hover 能力的设备上 mouseover 已经先把浮层打开，而 click 分支是
//     「is-open 取反」的纯开关，于是用户「移上去看到浮层 → 点一下」得到的必然是「关掉」
//     （触屏合成点击因为鼠标已在按钮上，也会落进同一条路径）。
//   · F2 —— ui/drawer.js 的 Esc 处理挂在 document 的冒泡阶段且**不看浮层**，浮层开着时
//     它先把整个抽屉关掉（closeDrawer 里 body.innerHTML = '' 连带清掉浮层节点），
//     「Esc 只关浮层并把焦点还给触发按钮」永远不可达（实测焦点掉到 body）。
//
// 为什么钉**源码形态**而不是行为：本仓库没有浏览器内 JS 测试基建（public/test 下的用例
// 要么走 Go/HTTP，要么只读页面产物），而这两条的判据恰好是事件阶段与状态语义 ——
// 捕获阶段（addEventListener 第三参数 true）、pin 语义（is-pinned）、以及 mouseout 对
// pin 的豁免。形态断言能防住「被改回旧写法」，交互正确性仍由真实浏览器实测背书：
// 修后读数（1440/768/375 三档 + hover/click/键盘/Esc 四路）见任务报告。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// adminJsSource 读取后台脚本源码。
//
// 用 runtime.Caller 定位本文件再回推到仓库根，而不是写死相对路径：
// go test 的工作目录是**包目录**，相对层数随测试文件放哪一层而变，
// 写错只会在运行期报「文件不存在」（而它看起来像 admin.js 丢了）。
func adminJsSource(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	// public/test/navigation/unit/<file> → 上溯 4 层到仓库根。
	root := filepath.Join(filepath.Dir(self), "..", "..", "..", "..")
	p := filepath.Join(root, "internal", "templates", "static", "js", "admin.js")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 admin.js 失败: %v", err)
	}
	return string(src)
}

// TestPanelPreviewClickPinsInsteadOfToggling F1：click 的语义必须是「固定/取消固定」，
// 不能退回「is-open 取反」——后者在 hover 设备上必然表现为「看到浮层的一瞬间点它就关」。
func TestPanelPreviewClickPinsInsteadOfToggling(t *testing.T) {
	src := adminJsSource(t)
	// 旧写法必须不再出现：它是 F1 的成因。
	if strings.Contains(src, "var willOpen = !root.classList.contains('is-open')") {
		t.Error("admin.js 的 click 分支退回「is-open 取反」的纯开关（F1 复发的形状）：hover 打开后点击会把它关掉")
	}
	// pin 语义三个必备点：状态、click 判据、mouseout 豁免。
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
	// 无 hover 的设备上 click 是唯一开合路径：必须仍然走同一段实现（不能只在 canHover 分支里打开）。
	if !strings.Contains(src, "if (canHover) {") {
		t.Error("admin.js 缺少 canHover 判定 —— hover 路径必须与 click 路径显式分开")
	}
}

// TestPanelPreviewEscapePreemptsDrawer F2：浮层的 Esc 必须在捕获阶段抢占，
// 并阻止事件继续传播（否则 ui/drawer.js 的冒泡阶段处理会先把抽屉关掉）。
func TestPanelPreviewEscapePreemptsDrawer(t *testing.T) {
	src := adminJsSource(t)
	// 锚点用**只有浮层那段 keydown 才有**的语句（查询「打开中的浮层」）。
	//
	// 不能用 document.addEventListener('keydown', …) 当锚点：admin.js 里还有其它键盘处理
	//（侧栏 / 页签），strings.Index 取到的是第一处，断言会在别的代码上通过 —— 变异验证
	//（把捕获阶段的第三参数删掉）第一版就是这么漏过的。
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
		"}, true);", // 第三参数 true = 捕获阶段：必须先于 drawer.js 的冒泡处理
	} {
		if !strings.Contains(tail, want) {
			t.Errorf("浮层 Esc 处理缺少 %q（在锚点之后 700 字符内未找到）—— Esc 的浮层优先权失效："+
				"抽屉会先把浮层一起关掉、焦点掉到 body", want)
		}
	}
}
