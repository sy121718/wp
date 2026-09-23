package templates

// workbench_slot_frame_test.go — 结构槽位（页眉 / 页脚）的编辑器契约。
//
// 背景：页眉页脚是**编译期注入**的节点（core.layoutSlot，发布与预览走同一条装配管线），
// 页面文档里没有它们。于是编辑器里「能不能认出这一段是页眉」「点它进的是不是全局块」
// 全靠前端三处配合，而这三处任何一处缺失都只是**静默失效**：
//   - 桥接层不识别 → 点页眉没有任何反应（它在 AST 里查不到，select() 静默返回）；
//   - 父窗口不分流 → 打开的是组件检查器，删除 / 拖动按钮对着一个不存在的节点；
//   - 块面板不设防 → 把页眉块拖进页面，得到「文档里一份 + 站点结构一份」两个页眉。
//
// Go 侧测试看不见这三条（服务端渲染与保存路径都正常），所以这里对源码断言契约。
// 断言的是**分支存在**，不是文案与样式。

import (
	"strings"
	"testing"
)

// TestWorkbenchSlotFrameBridgeContract 桥接层：槽位可识别、不可拖、上报槽位选中。
func TestWorkbenchSlotFrameBridgeContract(t *testing.T) {
	bridge := readTemplateFile(t, "../module/workbench/inbound/http/editor_bridge.go")

	for _, want := range []string{
		"[data-sky-slot]",        // 识别槽位元素（含降级占位）
		"wb-slot-select",         // 上报槽位选中（父窗口据此分流）
		"data-sky-slot-ref",      // 带出生效绑定，父窗口才能给出「编辑全局块」入口
		"data-sky-slot-ref-kind", // block / template 分流跳转目标
	} {
		if !strings.Contains(bridge, want) {
			t.Fatalf("编辑器桥接脚本应包含 %q", want)
		}
	}
	// 槽位子树不可拖、不接收落点：它不属于本页 AST，拖它只会得到一次无人响应的 moveNode。
	if !strings.Contains(bridge, "el.setAttribute('draggable', 'false')") {
		t.Fatalf("槽位元素应显式禁用拖拽")
	}
	// 就地改文本必须跳过槽位：改的要是全局块，页内副本会与站点结构那份分叉。
	if n := strings.Count(bridge, "closest('[data-sky-slot]')"); n < 4 {
		t.Fatalf("点击 / 双击 / 右键 / 拖放四条路径都要跳过槽位，实际命中 %d 处", n)
	}
}

// TestWorkbenchSlotFrameSelectionFlow 父窗口：槽位选中走独立分支，面板给的是「编辑全局块」。
func TestWorkbenchSlotFrameSelectionFlow(t *testing.T) {
	shortcuts := readTemplateFile(t, "static/js/workbench/methods/shortcuts.js")
	if !strings.Contains(shortcuts, "wb-slot-select") || !strings.Contains(shortcuts, "selectSlotFrame") {
		t.Fatalf("父窗口应把 wb-slot-select 分流到 selectSlotFrame（否则槽位选中走 select() 静默失败）")
	}

	nodes := readTemplateFile(t, "static/js/workbench/methods/nodes.js")
	if !strings.Contains(nodes, "selectSlotFrame(") {
		t.Fatalf("缺少 selectSlotFrame：槽位选中态无法记录")
	}
	if !strings.Contains(nodes, "this.selectedSlot = null;") {
		t.Fatalf("选中普通节点应清掉槽位态（两者互斥，否则面板会显示上一个槽位）")
	}

	panels := readTemplateFile(t, "static/js/workbench/methods/panels.js")
	if !strings.Contains(panels, "renderSlotPanel(") {
		t.Fatalf("缺少 renderSlotPanel：点页眉打开的会是组件检查器")
	}
	for _, want := range []string{"'/workbench?block='", "'/workbench?template='", "returnUrl="} {
		if !strings.Contains(panels, want) {
			t.Fatalf("槽位面板应提供跳转入口 %s（块与结构模板的编辑入口不同）", want)
		}
	}
	// 槽位不是本页节点：隐藏 / 锁定 / 删除对它没有意义。
	if !strings.Contains(panels, "wb-edit-delete") {
		t.Fatalf("槽位面板应隐藏删除按钮（它对编译期注入的边界无效）")
	}
}

// TestWorkbenchSlotBlockReuseGuard 护栏：已被站点结构占用的块不得再插进页面文档。
//
// 这是「两个页眉」的根因入口 —— 编译期去重只认 core.layoutSlot 节点，手插的是
// core.globalref，两者互不识别。块面板置灰是**第一道**，insertComponent 的拦截是
// 兜底（拖拽、粘贴等所有插入路径都汇到这里）。
func TestWorkbenchSlotBlockReuseGuard(t *testing.T) {
	canvas := readTemplateFile(t, "static/js/workbench/methods/canvas.js")
	if !strings.Contains(canvas, "slotRoleOf(") {
		t.Fatalf("缺少 slotRoleOf：无法判断某块是否已被站点结构占用")
	}
	if !strings.Contains(canvas, "is-slot-locked") {
		t.Fatalf("块面板应把槽位块标成禁用态（一眼可见，而不是插入后才报错）")
	}
	if !strings.Contains(canvas, "if (slotRole) {") {
		t.Fatalf("insertComponent 应拦截槽位块（拖拽等路径绕不过块面板的置灰）")
	}
	if !strings.Contains(canvas, "setNotice(") {
		t.Fatalf("拦截后要有反馈出口（状态栏提示），否则表现为「点了没反应」")
	}
}

// TestWorkbenchSlotFrameTemplate 组件模板：标记层带齐画布需要的三个属性。
//
// 属性名与前端（bridge 的读取、父窗口的分流）是跨语言契约：改一边不改另一边
// 会得到「点页眉只高亮、进不去编辑」这种半截行为。
func TestWorkbenchSlotFrameTemplate(t *testing.T) {
	jet := readTemplateFile(t, "../builder/components/layoutslot/layoutslot.jet")
	for _, want := range []string{
		`data-sky-slot="{{ .SlotFrame.Slot }}"`,
		`data-sky-slot-ref="{{ .SlotFrame.Ref }}"`,
		`data-sky-slot-ref-kind="{{ .SlotFrame.RefKind }}"`,
	} {
		if !strings.Contains(jet, want) {
			t.Fatalf("槽位模板应输出 %s", want)
		}
	}
}
