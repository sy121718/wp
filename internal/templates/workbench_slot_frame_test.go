package templates

import (
	"strings"
	"testing"
)

func TestWorkbenchSlotFrameBridgeContract(t *testing.T) {
	bridge := readTemplateFile(t, "../module/workbench/inbound/http/scripts/editor_bridge.js")

	for _, want := range []string{
		"[data-sky-slot]",
		"wb-slot-select",
		"data-sky-slot-ref",
		"data-sky-slot-ref-kind",
	} {
		if !strings.Contains(bridge, want) {
			t.Fatalf("编辑器桥接脚本应包含 %q", want)
		}
	}

	if !strings.Contains(bridge, "el.setAttribute('draggable', 'false')") {
		t.Fatalf("槽位元素应显式禁用拖拽")
	}

	if n := strings.Count(bridge, "closest('[data-sky-slot]')"); n < 4 {
		t.Fatalf("点击 / 双击 / 右键 / 拖放四条路径都要跳过槽位，实际命中 %d 处", n)
	}
}

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

	if !strings.Contains(panels, "if (this.selectedSlot) { this.renderSlotPanel(); return; }") {
		t.Fatalf("槽位必须先进入独立面板，不能挂载普通节点的内容操作")
	}
}

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

func TestWorkbenchSlotFrameTemplate(t *testing.T) {
	jet := readTemplateFile(t, "../../pkg/builder/components/layoutslot/layoutslot.jet")
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
