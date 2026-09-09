// canvas_align_contract_test.go — 画布路径「子节点增删改 → props 数组同步」契约测试。
//
// 背景：tabs / accordion 的校验是「标签数 = 面板数」（tabs.go:66 / accordion.go:68），
// 而画布上的删除面板、拖拽重排、拖入新面板、复制 / 粘贴面板都只改 node.children，
// 过去不同步 props 数组 —— 删掉一个面板就立刻「标签数与面板数不一致」编译失败。
// 修复把画布方向收敛到 palette.js 的 alignFromChildren（无 DOM 纯函数，与检查器方向的
// alignMutation 对称），由 methods/canvas.js 与 methods/nodes.js 的画布入口调用。
//
// 本测试用 node 加载真实的 canvas.js / nodes.js（只桩掉渲染与持久化），直接调用画布上
// 真正跑的 insertComponent / deleteSelected / moveNode / moveNodeOrder / pasteAfter /
// duplicate，再把真实产物交给 Go 侧 Validate / Compile —— 不存在「测试里另抄一份画布
// 逻辑」的漂移。环境没有 node 时自动跳过。

package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// canvasJSRel / nodesJSRel 画布逻辑文件相对本包测试工作目录的路径。
const (
	canvasJSRel = "../templates/static/js/workbench/methods/canvas.js"
	nodesJSRel  = "../templates/static/js/workbench/methods/nodes.js"
)

// canvasAlignCase 一个画布操作序列执行后的文档片段（node 侧探针的真实产物）。
type canvasAlignCase struct {
	// Name 场景名（子测试名）。
	Name string `json:"name"`
	// Type 结构型组件类型。
	Type string `json:"type"`
	// Key 内容数组的 props 键（tabs / items；非结构型为空）。
	Key string `json:"key"`
	// Node 操作序列执行后的节点。
	Node *core.Node `json:"node"`
	// Entries 脚本侧统计的内容数组长度。
	Entries int `json:"entries"`
	// ChildIDs 操作后的子节点 ID 顺序。
	ChildIDs []string `json:"childIds"`
	// Labels 操作后的标签 / 标题顺序。
	Labels []string `json:"labels"`

	ExpectedChildIDs []string `json:"expectedChildIDs"`
	ExpectedLabels   []string `json:"expectedLabels"`
	RemovedChildID   string   `json:"removedChildID"`
	MovedID          string   `json:"movedID"`
	RootCount        int      `json:"rootCount"`
	PropsUntouched   bool     `json:"propsUntouched"`
	InspectorSynced  bool     `json:"inspectorSynced"`
	IllegalAllNull   bool     `json:"illegalAllNull"`
}

// canvasAlignProbe node 侧探针结果。
type canvasAlignProbe struct {
	Cases []canvasAlignCase `json:"cases"`
}

// canvasAlignDirty 故意构造的「不同步」坏状态：数量本就不一致，必须被校验拒绝，
// 且画布操作不得去猜位置改写 props。
func canvasAlignDirty(name string) bool {
	return strings.HasPrefix(name, "tabs-dirty-") || name == "canvas-delete-without-sync-dirty"
}

// runCanvasAlignProbe 用 node 加载真实画布模块并跑完所有操作场景。
func runCanvasAlignProbe(t *testing.T) (p canvasAlignProbe) {
	t.Helper()
	urls := make([]string, 0, 3)
	for _, rel := range []string{paletteJSRel, canvasJSRel, nodesJSRel} {
		abs, err := filepath.Abs(rel)
		if err != nil {
			t.Fatalf("解析 %s 路径失败: %v", rel, err)
		}
		if _, err = os.Stat(abs); err != nil {
			t.Fatalf("%s 不存在: %v", rel, err)
		}
		urls = append(urls, "file://"+filepath.ToSlash(abs))
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未找到 node，跳过画布对齐契约测试")
	}
	out, err := exec.Command(nodeBin, "--input-type=module", "--eval",
		fmt.Sprintf(canvasAlignProbeScript, urls[0], urls[1], urls[2])).CombinedOutput()
	if err != nil {
		t.Fatalf("node 求值画布路径失败: %v\n%s", err, out)
	}
	if err = json.Unmarshal(out, &p); err != nil {
		t.Fatalf("解析画布探针 JSON 失败: %v\n%s", err, out)
	}
	if len(p.Cases) == 0 {
		t.Fatal("画布探针没有产出任何场景")
	}
	return p
}

// canvasAlignField 内容数组的文案字段（tabs→label，accordion→title）。
func canvasAlignField(typ string) string {
	if typ == "core.accordion" {
		return "title"
	}
	return "label"
}

// canvasAlignProbeScript node 侧探针：加载真实 canvas.js / nodes.js（只桩掉渲染与持久化），
// 驱动画布上的真实方法，逐个场景输出操作后的节点。
const canvasAlignProbeScript = `
// 画布路径（拖入 / 删除 / 重排子节点）→ props 数组同步的探针。
// 与浏览器共用同一份实现：这里 import 的是真实的 canvas.js / nodes.js（只桩掉渲染与持久化），
// 所以测的是画布上真正跑的那段代码，不是测试里另抄的一份逻辑。
globalThis.document = {
  getElementById: function () { return { textContent: '{}' }; },
  querySelector: function () { return null; },
  querySelectorAll: function () { return []; },
  createElement: function () { return { style: {}, dataset: {}, classList: { add: function(){}, remove: function(){}, toggle: function(){} }, appendChild: function(){}, addEventListener: function(){}, querySelector: function(){ return null; }, setAttribute: function(){} }; },
  addEventListener: function () {}, removeEventListener: function () {}, body: { appendChild: function () {} }
};
globalThis.window = { location: { origin: 'http://localhost' } };

const palette = await import(%q);
const { canvasMethods } = await import(%q);
const { nodesMethods } = await import(%q);
const paletteItems = palette.paletteItems;
const alignKeyOf = palette.alignKeyOf;
const alignMutation = palette.alignMutation;
const alignFromChildren = palette.alignFromChildren;

function itemOf(type) {
  return paletteItems.filter(function (e) { return e.type === type; })[0];
}
function makeWB() {
  var wb = Object.assign({}, nodesMethods, canvasMethods);
  wb.doc = { settings: { layout: { mode: 'full' } }, root: [] };
  wb.selectedId = '';
  wb.undoStack = []; wb.redoStack = []; wb.clipboard = null; wb.saveState = '';
  // 只桩掉渲染 / 持久化 / 画布提交；节点查找与 AST 变更全部是真实实现。
  wb.snapshot = function () { wb.undoStack.push(JSON.stringify(wb.doc)); };
  wb.renderTree = function () {}; wb.renderUI = function () {};
  wb.syncInspector = function () {}; wb.refreshCanvas = function () {};
  wb.patchCanvas = function () {}; wb.backupDoc = function () {};
  return wb;
}
// insertRoot 与画布「未选中任何节点时点击组件库」一致：插到顶级。
function insertRoot(wb, type) {
  wb.selectedId = '';
  wb.insertComponent(itemOf(type));
  return wb.doc.root[wb.doc.root.length - 1];
}
function ids(n) { return (n.children || []).map(function (c) { return c.id; }); }
function entries(n) { return n.props[alignKeyOf(n.type)] || []; }
function fieldOf(n) { return n.type === 'core.tabs' ? 'label' : 'title'; }
function labels(n) { return entries(n).map(function (x) { return x[fieldOf(n)]; }); }
function snap(name, n, extra) {
  // 深拷贝：多个用例共用同一个节点对象时，最后序列化的是最终状态，会互相污染。
  var out = { name: name, type: n.type, key: alignKeyOf(n.type), node: JSON.parse(JSON.stringify(n)),
    entries: entries(n).length, childIds: ids(n), labels: labels(n) };
  Object.keys(extra || {}).forEach(function (k) { out[k] = extra[k]; });
  return out;
}
var cases = [];

// 1) 组件库拖到第 1 个面板之前 → 新增面板 + 新增标签（下标 0）
var wb1 = makeWB(); var t1 = insertRoot(wb1, 'core.tabs');
var before1 = labels(t1);
wb1.insertComponent(itemOf('core.text'), t1.children[0].id, 'before');
cases.push(snap('tabs-insert-before-panel', t1, { expectedLabels: ['页签1', before1[0]] }));

// 2) 再拖到末尾面板之后 → 追加面板 + 标签
wb1.insertComponent(itemOf('core.text'), t1.children[t1.children.length - 1].id, 'after');
cases.push(snap('tabs-insert-after-panel', t1));

// 3) 直接落点 inside 到 tabs（画布桥接在元素中带就发 inside）→ 追加面板 + 标签
var wb2 = makeWB(); var t2 = insertRoot(wb2, 'core.tabs');
wb2.insertComponent(itemOf('core.container'), t2.id, 'inside');
cases.push(snap('tabs-insert-inside', t2));

// 4) 删除中间面板 → 标签同步少一条，剩余顺序不变
var wb3 = makeWB(); var t3 = insertRoot(wb3, 'core.tabs');
wb3.insertComponent(itemOf('core.text'), t3.children[0].id, 'after');
wb3.insertComponent(itemOf('core.text'), t3.children[t3.children.length - 1].id, 'after');
var ids3 = ids(t3); var labels3 = labels(t3);
wb3.selectedId = ids3[1]; wb3.deleteSelected();
cases.push(snap('tabs-delete-middle-panel', t3, {
  expectedChildIDs: [ids3[0], ids3[2]], expectedLabels: [labels3[0], labels3[2]], removedChildID: ids3[1]
}));

// 5) 同层上移（moveNodeOrder）→ 标签顺序跟随
var wb4 = makeWB(); var t4 = insertRoot(wb4, 'core.tabs');
wb4.insertComponent(itemOf('core.text'), t4.children[0].id, 'after');
wb4.insertComponent(itemOf('core.text'), t4.children[t4.children.length - 1].id, 'after');
var ids4 = ids(t4); var labels4 = labels(t4);
wb4.moveNodeOrder(ids4[2], -1);
cases.push(snap('tabs-move-node-order-up', t4, {
  expectedChildIDs: [ids4[0], ids4[2], ids4[1]], expectedLabels: [labels4[0], labels4[2], labels4[1]]
}));

// 6) moveNode 同父重排：第 1 个面板拖到第 3 个之后 → 标签一起换位
var wb5 = makeWB(); var t5 = insertRoot(wb5, 'core.tabs');
wb5.insertComponent(itemOf('core.text'), t5.children[0].id, 'after');
wb5.insertComponent(itemOf('core.text'), t5.children[t5.children.length - 1].id, 'after');
var ids5 = ids(t5); var labels5 = labels(t5);
wb5.moveNode(ids5[0], ids5[2], 'after');
cases.push(snap('tabs-move-node-same-parent', t5, {
  expectedChildIDs: [ids5[1], ids5[2], ids5[0]], expectedLabels: [labels5[1], labels5[2], labels5[0]]
}));

// 7) moveNode 把面板拖出 tabs（落到根节点之后）→ 标签同步少一条
var wb6 = makeWB(); var t6 = insertRoot(wb6, 'core.tabs');
wb6.insertComponent(itemOf('core.text'), t6.children[0].id, 'after');
var rootSibling = insertRoot(wb6, 'core.container');
var ids6 = ids(t6); var labels6 = labels(t6);
wb6.moveNode(ids6[1], rootSibling.id, 'after');
cases.push(snap('tabs-move-node-out', t6, {
  expectedChildIDs: [ids6[0]], expectedLabels: [labels6[0]], rootCount: wb6.doc.root.length
}));

// 8) 画布桥接中带落点：把根节点拖到 tabs 的中间（placement=inside）→ 新增面板 + 标签
var wb7 = makeWB(); var t7 = insertRoot(wb7, 'core.tabs');
var moving = insertRoot(wb7, 'core.container');
wb7.moveNode(moving.id, t7.id, 'inside');
cases.push(snap('tabs-move-node-inside', t7, { movedID: moving.id }));

// 9) 面板拖到自身父 tabs 的中间（inside）→ 等于移到末尾，标签顺序跟随
var wb14 = makeWB(); var t14 = insertRoot(wb14, 'core.tabs');
wb14.insertComponent(itemOf('core.text'), t14.children[0].id, 'after');
var ids14 = ids(t14); var labels14 = labels(t14);
wb14.moveNode(ids14[0], t14.id, 'inside');
cases.push(snap('tabs-move-node-self-inside', t14, {
  expectedChildIDs: [ids14[1], ids14[0]], expectedLabels: [labels14[1], labels14[0]]
}));

// 10) duplicate（Ctrl+D 复制面板）→ 新增面板 + 标签
var wb8 = makeWB(); var t8 = insertRoot(wb8, 'core.tabs');
wb8.selectedId = t8.children[0].id;
wb8.duplicate();
cases.push(snap('tabs-duplicate-panel', t8));

// 11) 复制后「粘贴到下方」→ 新增面板 + 标签
var wb9 = makeWB(); var t9 = insertRoot(wb9, 'core.tabs');
wb9.selectedId = t9.children[0].id;
wb9.copyNode();
wb9.pasteAfter(t9.children[0].id);
cases.push(snap('tabs-paste-after-panel', t9));

// 12) accordion 混合流程：拖入 + 上移
var wb10 = makeWB(); var a1 = insertRoot(wb10, 'core.accordion');
wb10.insertComponent(itemOf('core.text'), a1.children[0].id, 'after');
wb10.insertComponent(itemOf('core.text'), a1.children[a1.children.length - 1].id, 'after');
var idsA = ids(a1); var labelsA = labels(a1);
wb10.moveNodeOrder(idsA[2], -1);
cases.push(snap('accordion-mixed', a1, {
  expectedChildIDs: [idsA[0], idsA[2], idsA[1]], expectedLabels: [labelsA[0], labelsA[2], labelsA[1]]
}));

// 13) accordion 删除折叠项 → 标题同步少一条
var wb15 = makeWB(); var a2 = insertRoot(wb15, 'core.accordion');
wb15.insertComponent(itemOf('core.text'), a2.children[0].id, 'after');
var idsA2 = ids(a2); var labelsA2 = labels(a2);
wb15.selectedId = idsA2[0]; wb15.deleteSelected();
cases.push(snap('accordion-delete-first', a2, {
  expectedChildIDs: [idsA2[1]], expectedLabels: [labelsA2[1]]
}));

// 14) 两个方向不漂移：画布删掉一个面板后，检查器「+ 添加」仍能把数量补回一致
var wb16 = makeWB(); var t16 = insertRoot(wb16, 'core.tabs');
wb16.insertComponent(itemOf('core.text'), t16.children[0].id, 'after');
var ids16 = ids(t16);
wb16.selectedId = ids16[0]; wb16.deleteSelected();          // 画布方向（alignFromChildren）
var key16 = alignKeyOf(t16.type);
var add16 = alignMutation(t16.type, t16.props[key16], t16.children, { op: 'add' }, wb16.makeIdAllocator());
if (add16) { t16.props[key16] = add16.list; t16.children = add16.children; }
cases.push(snap('tabs-canvas-then-inspector', t16, { inspectorSynced: !!add16 }));

// 15) 脏数据（props 多一条）→ 删除面板时不同步，保持原样
var wb11 = makeWB(); var d1 = insertRoot(wb11, 'core.tabs');
wb11.insertComponent(itemOf('core.text'), d1.children[0].id, 'after');
d1.props.tabs.push({ label: '多余标签' });
var dirty1 = JSON.stringify(d1.props.tabs);
wb11.selectedId = d1.children[0].id; wb11.deleteSelected();
cases.push(snap('tabs-dirty-skip-remove', d1, { propsUntouched: JSON.stringify(d1.props.tabs) === dirty1 }));

// 16) 脏数据（props 少一条）→ 拖入也不猜位置
var wb12 = makeWB(); var d2 = insertRoot(wb12, 'core.tabs');
d2.props.tabs = [];
var dirty2 = JSON.stringify(d2.props.tabs);
wb12.insertComponent(itemOf('core.text'), d2.children[0].id, 'after');
cases.push(snap('tabs-dirty-skip-insert', d2, { propsUntouched: JSON.stringify(d2.props.tabs) === dirty2 }));

// 17) 非结构型组件的子节点增删：props 不得被改写
var wb13 = makeWB(); var c1 = insertRoot(wb13, 'core.container');
var props13 = JSON.stringify(c1.props);
wb13.insertComponent(itemOf('core.text'), c1.id, 'inside');
cases.push({ name: 'container-untouched', type: c1.type, key: alignKeyOf(c1.type),
  node: c1, propsUntouched: JSON.stringify(c1.props) === props13 });

// 19) 回归护栏：修复前的坏行为（画布直接删子节点、不同步 props）必须被校验拒绝
var wb17 = makeWB(); var bad = insertRoot(wb17, 'core.tabs');
wb17.insertComponent(itemOf('core.text'), bad.children[0].id, 'after'); // 2 面板 / 2 标签
bad.children.splice(0, 1);   // 只动 children，不动 props.tabs —— 旧代码的行为
cases.push(snap('canvas-delete-without-sync-dirty', bad));

// 18) 纯函数非法输入必须返回 null
var illegal = [
  alignFromChildren('core.text', [], [], { op: 'insert', index: 0 }),
  alignFromChildren('core.tabs', [], [], { op: 'remove', index: 0 }),
  alignFromChildren('core.tabs', [{ label: 'a' }], [{}], { op: 'move', index: 0, to: 0 }),
  alignFromChildren('core.tabs', [{ label: 'a' }], [{}, {}], { op: 'insert', index: 5 }),
  alignFromChildren('core.tabs', [], [], { op: 'nope' })
];
cases.push({ name: 'illegal-ops-return-null', illegalAllNull: illegal.every(function (x) { return x === null; }) });

console.log(JSON.stringify({ cases: cases }));`

// TestCanvasChildOpsKeepAlignArrayInSync 核心不变量：画布上的拖入 / 删除 / 重排 /
// 复制粘贴执行后，props 数组长度必须等于 children 数量，且节点仍可编译。
func TestCanvasChildOpsKeepAlignArrayInSync(t *testing.T) {
	for _, c := range runCanvasAlignProbe(t).Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			switch c.Name {
			case "illegal-ops-return-null":
				if !c.IllegalAllNull {
					t.Error("纯函数对非法输入（非结构型 / 越界 / 原位移动 / 未知操作）没有全部返回 null")
				}
				return
			case "container-untouched":
				if c.Node == nil {
					t.Fatal("探针没有返回节点")
				}
				if !c.PropsUntouched {
					t.Error("非结构型组件的 props 被画布子节点操作改写了")
				}
				if err := core.ValidateNode(c.Node, map[string]bool{}); err != nil {
					t.Errorf("容器插入子节点后校验失败: %v", err)
				}
				return
			}
			if c.Node == nil {
				t.Fatal("探针没有返回节点")
			}
			if c.Type != "core.tabs" && c.Type != "core.accordion" {
				t.Fatalf("意外的组件类型: %s", c.Type)
			}
			if c.Key == "" {
				t.Fatalf("%s 没有内容数组键", c.Type)
			}
			goEntries := alignedEntryCount(t, c.Node, c.Key)
			if goEntries != c.Entries {
				t.Errorf("内容数组长度不一致：Go 侧 %d / 脚本侧 %d", goEntries, c.Entries)
			}
			childIDs := make([]string, 0, len(c.Node.Children))
			for _, k := range c.Node.Children {
				childIDs = append(childIDs, k.ID)
			}
			if !sameStrings(childIDs, c.ChildIDs) {
				t.Errorf("子节点 ID 顺序不一致：%v / %v", childIDs, c.ChildIDs)
			}
			if _, dup := alignedSubtreeIDs(c.Node); dup != "" {
				t.Errorf("子树内 ID 重复: %s", dup)
			}
			labels := alignedLabels(t, c.Node, c.Key, canvasAlignField(c.Type))
			if !sameStrings(labels, c.Labels) {
				t.Errorf("标签 / 标题顺序不一致：%v / %v", labels, c.Labels)
			}

			if canvasAlignDirty(c.Name) {
				// 脏数据场景：数量必须仍然不一致，且校验必须拦下（护栏没有失效）。
				if goEntries == len(c.Node.Children) {
					t.Fatalf("脏数据场景竟然数量一致（%d），护栏用例失效", goEntries)
				}
				err := core.ValidateNode(c.Node, map[string]bool{})
				if err == nil {
					t.Fatal("标签数与面板数不一致竟然通过校验（校验规则被削弱？）")
				}
				if !strings.Contains(err.Error(), "需与面板数量") && !strings.Contains(err.Error(), "需与内容数") {
					t.Errorf("报错信息不符: %v", err)
				}
			} else {
				if goEntries != len(c.Node.Children) {
					t.Errorf("画布操作后数量不一致：props.%s %d 个 / 子节点 %d 个", c.Key, goEntries, len(c.Node.Children))
				}
				for i, l := range labels {
					if l == "" {
						t.Errorf("第 %d 条标签 / 标题为空（校验要求非空）", i+1)
					}
				}
				if err := core.ValidateNode(c.Node, map[string]bool{}); err != nil {
					t.Errorf("画布操作后校验失败: %v", err)
				}
			}

			switch c.Name {
			case "tabs-insert-before-panel":
				if !sameStrings(labels, c.ExpectedLabels) {
					t.Errorf("按下标插入的标签位置不符：%v，期望 %v", labels, c.ExpectedLabels)
				}
			case "tabs-insert-after-panel":
				if goEntries != 3 || len(c.Node.Children) != 3 {
					t.Errorf("连续两次拖入后应为 3 / 3，实际 %d / %d", goEntries, len(c.Node.Children))
				}
			case "tabs-insert-inside", "tabs-move-node-inside":
				if goEntries != 2 || len(c.Node.Children) != 2 {
					t.Errorf("拖入内部后应为 2 / 2，实际 %d / %d", goEntries, len(c.Node.Children))
				}
				if c.MovedID != "" && !sameStrings([]string{childIDs[len(childIDs)-1]}, []string{c.MovedID}) {
					t.Errorf("拖入内部的节点不在末尾：%v", childIDs)
				}
			case "tabs-delete-middle-panel", "accordion-delete-first":
				if !sameStrings(childIDs, c.ExpectedChildIDs) {
					t.Errorf("删除后子节点顺序不符：%v，期望 %v", childIDs, c.ExpectedChildIDs)
				}
				if c.RemovedChildID != "" {
					for _, id := range childIDs {
						if id == c.RemovedChildID {
							t.Errorf("被删面板对应的子节点仍在文档中: %s", id)
						}
					}
				}
			case "tabs-move-node-order-up", "tabs-move-node-same-parent", "tabs-move-node-self-inside", "accordion-mixed":
				if !sameStrings(childIDs, c.ExpectedChildIDs) {
					t.Errorf("重排后子节点顺序不符：%v，期望 %v", childIDs, c.ExpectedChildIDs)
				}
				if !sameStrings(labels, c.ExpectedLabels) {
					t.Errorf("重排后标签顺序不符：%v，期望 %v", labels, c.ExpectedLabels)
				}
			case "tabs-move-node-out":
				if !sameStrings(childIDs, c.ExpectedChildIDs) {
					t.Errorf("拖出后子节点不符：%v，期望 %v", childIDs, c.ExpectedChildIDs)
				}
			case "tabs-duplicate-panel", "tabs-paste-after-panel":
				if goEntries != 2 || len(c.Node.Children) != 2 {
					t.Errorf("复制 / 粘贴面板后应为 2 / 2，实际 %d / %d", goEntries, len(c.Node.Children))
				}
			case "tabs-canvas-then-inspector":
				if !c.InspectorSynced {
					t.Error("画布删除后面板后，检查器「+ 添加」没有把数量补回一致（两个方向漂移）")
				}
			case "tabs-dirty-skip-remove", "tabs-dirty-skip-insert":
				if !c.PropsUntouched {
					t.Error("脏数据场景下画布操作改写了 props 数组（应当保守跳过）")
				}
			}
		})
	}
}

// TestCanvasChildOpsNodesCompile 每个画布操作序列的产物都必须能真正编译出 HTML。
func TestCanvasChildOpsNodesCompile(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	for _, c := range runCanvasAlignProbe(t).Cases {
		c := c
		if c.Node == nil || canvasAlignDirty(c.Name) {
			continue // 纯函数护栏用例无节点；脏数据场景编译必然失败
		}
		t.Run(c.Name, func(t *testing.T) {
			doc, err := json.Marshal(map[string]any{
				"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
				"root":     []*core.Node{c.Node},
			})
			if err != nil {
				t.Fatalf("序列化文档失败: %v", err)
			}
			page, err := ParsePage(doc)
			if err != nil {
				t.Fatalf("文档解析失败: %v", err)
			}
			if _, err = Compile(page, WithComponentSet(set)); err != nil {
				t.Errorf("%s 操作后编译失败: %v", c.Name, err)
			}
		})
	}
}
