// aligned_contract_test.go — tabs / accordion「标签数组 ↔ children」同步契约测试。
//
// 背景：Inspector 的 tabsPanel / accordionPanel 过去加/删标签只改 props 数组、
// 不同步 children，于是「标签数与面板数不一致」立刻编译失败（tabs.go:58 /
// accordion.go:60 的硬校验）。修复把三种操作收敛到 palette.js 的 alignMutation
// （无 DOM 纯函数）：加标签 → 同步建默认面板；删标签 → 同步删面板；上移/下移 →
// 子节点同步重排。
//
// 本测试用 node 求值 palette.js（与浏览器点按钮走同一份实现），按检查器的操作序列
// 驱动 alignMutation，再把真实产物交给 Go 侧 Validate / Compile —— 不存在「测试里
// 抄一份逻辑」的漂移。环境没有 node 时自动跳过。

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

// alignedCase 一个操作序列跑完后的文档片段（node 求值 alignMutation 的产物）。
type alignedCase struct {
	// Name 场景名（子测试名）。
	Name string `json:"name"`
	// Type 结构型组件类型。
	Type string `json:"type"`
	// Key 内容数组的 props 键（tabs / items）。
	Key string `json:"key"`
	// Node 操作序列执行后的节点（真实产物）。
	Node *core.Node `json:"node"`
	// Entries 脚本侧统计的内容数组长度（与 Go 侧解析结果交叉校验）。
	Entries int `json:"entries"`
	// ChildIDs 操作后的子节点 ID 顺序。
	ChildIDs []string `json:"childIds"`

	AddedChildID     string   `json:"addedChildID"`
	KeptChildID      string   `json:"keptChildID"`
	RemovedChildID   string   `json:"removedChildID"`
	ExpectedChildIDs []string `json:"expectedChildIDs"`
	ExpectedLabels   []string `json:"expectedLabels"`
	IllegalAllNull   bool     `json:"illegalAllNull"`
}

// alignedProbe node 侧探针结果。
type alignedProbe struct {
	Cases []alignedCase `json:"cases"`
}

// runAlignedProbe 用 node 求值 palette.js 并跑完所有操作场景。
func runAlignedProbe(t *testing.T) alignedProbe {
	t.Helper()
	abs, err := filepath.Abs(paletteJSRel)
	if err != nil {
		t.Fatalf("解析 palette.js 路径失败: %v", err)
	}
	if _, err = os.Stat(abs); err != nil {
		t.Fatalf("palette.js 不存在: %v", err)
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未找到 node，跳过 tabs/accordion 对齐契约测试")
	}
	url := "file://" + filepath.ToSlash(abs)
	out, err := exec.Command(nodeBin, "--input-type=module", "--eval", fmt.Sprintf(alignedProbeScript, url)).CombinedOutput()
	if err != nil {
		t.Fatalf("node 求值 alignMutation 失败: %v\n%s", err, out)
	}
	var p alignedProbe
	if err = json.Unmarshal(out, &p); err != nil {
		t.Fatalf("解析对齐探针 JSON 失败: %v\n%s", err, out)
	}
	if len(p.Cases) == 0 {
		t.Fatal("对齐探针没有产出任何场景")
	}
	return p
}

// alignedProbeScript node 侧探针：与浏览器检查器同一套调用（buildInsertNode +
// alignMutation + makeIdAllocator 语义），逐个场景输出操作后的真实节点。
const alignedProbeScript = `
import { paletteItems, buildInsertNode, alignMutation, alignKeyOf } from %q;

// findNode 与 workbench 的 findNode 同语义（递归查找文档内节点）。
function findNode(list, id) {
  for (var i = 0; i < (list || []).length; i++) {
    var n = list[i];
    if (n.id === id) return n;
    var hit = findNode(n.children, id);
    if (hit) return hit;
  }
  return null;
}

// makeAllocator 与 canvas.js 的 makeIdAllocator 同语义：本批内不重复，且不与文档已有 ID 冲突。
function makeAllocator(doc) {
  var used = {};
  return function (base) {
    var prefix = String(base || 'node').split('-')[0] || 'node';
    var n = 1;
    while (used[prefix + '-' + n] || findNode(doc.root, prefix + '-' + n)) n++;
    used[prefix + '-' + n] = true;
    return prefix + '-' + n;
  };
}

// insert 模拟「从组件库拖入一个结构型组件」。
function insert(type) {
  var item = paletteItems.filter(function (e) { return e.type === type; })[0];
  var doc = { root: [] };
  var node = buildInsertNode(item, makeAllocator(doc));
  doc.root.push(node);
  return { doc: doc, node: node };
}

// apply 模拟检查器点一次按钮：走纯函数，再落回 node（与 repeater.js 的 applyAligned 同序）。
function apply(c, action) {
  var key = alignKeyOf(c.node.type);
  var next = alignMutation(c.node.type, c.node.props[key], c.node.children || [], action, makeAllocator(c.doc));
  if (!next) return false;
  c.node.props[key] = next.list;
  c.node.children = next.children;
  return true;
}

function childIDs(node) { return (node.children || []).map(function (k) { return k.id; }); }
function entriesOf(node) { return (node.props[alignKeyOf(node.type)] || []).length; }
function labelsOf(node, field) {
  return (node.props[alignKeyOf(node.type)] || []).map(function (x) { return x[field]; });
}
function snapshot(name, c, extra) {
  var out = {
    name: name,
    type: c.node.type,
    key: alignKeyOf(c.node.type),
    node: c.node,
    entries: entriesOf(c.node),
    childIds: childIDs(c.node)
  };
  Object.keys(extra || {}).forEach(function (k) { out[k] = extra[k]; });
  return out;
}

var cases = [];

// 1) 加一个标签 → 面板同步 +1，原面板保留
var t1 = insert('core.tabs');
var before1 = childIDs(t1.node);
apply(t1, { op: 'add' });
cases.push(snapshot('tabs-add-1', t1, { addedChildID: childIDs(t1.node)[1], keptChildID: before1[0] }));

// 2) 连续加两个标签 → 数量一致，新增 ID 互不撞
var t2 = insert('core.tabs');
apply(t2, { op: 'add' });
apply(t2, { op: 'add' });
cases.push(snapshot('tabs-add-2', t2));

// 3) 删中间一个标签 → 面板同步删除，剩余顺序不变
var t3 = insert('core.tabs');
apply(t3, { op: 'add' });
apply(t3, { op: 'add' });
var ids3 = childIDs(t3.node);
apply(t3, { op: 'remove', index: 1 });
cases.push(snapshot('tabs-remove-middle', t3, { removedChildID: ids3[1], expectedChildIDs: [ids3[0], ids3[2]] }));

// 4) 下移第一个标签 → 标签与面板一起换位
var t4 = insert('core.tabs');
apply(t4, { op: 'add' });
apply(t4, { op: 'add' });
var ids4 = childIDs(t4.node);
var labels4 = labelsOf(t4.node, 'label');
apply(t4, { op: 'move', index: 0, to: 2 });
cases.push(snapshot('tabs-move-first-to-last', t4, {
  expectedChildIDs: [ids4[1], ids4[2], ids4[0]],
  expectedLabels: [labels4[1], labels4[2], labels4[0]]
}));

// 5) accordion 加一个折叠项 → 内容同步 +1
var a1 = insert('core.accordion');
apply(a1, { op: 'add' });
cases.push(snapshot('accordion-add-1', a1));

// 6) accordion 删第一个折叠项 → 内容同步删除
var a2 = insert('core.accordion');
apply(a2, { op: 'add' });
var idsA = childIDs(a2.node);
apply(a2, { op: 'remove', index: 0 });
cases.push(snapshot('accordion-remove-first', a2, { removedChildID: idsA[0], expectedChildIDs: [idsA[1]] }));

// 7) 非法操作必须返回 null 且不改动文档（越界删除 / 原位移动 / 非结构型组件）
var t5 = insert('core.tabs');
var illegal = [
  alignMutation('core.tabs', t5.node.props.tabs, t5.node.children, { op: 'remove', index: 9 }, makeAllocator(t5.doc)),
  alignMutation('core.tabs', t5.node.props.tabs, t5.node.children, { op: 'move', index: 0, to: 0 }, makeAllocator(t5.doc)),
  alignMutation('core.text', [], [], { op: 'add' }, makeAllocator(t5.doc))
];
cases.push(snapshot('illegal-ops-return-null', t5, {
  illegalAllNull: illegal.every(function (x) { return x === null; })
}));

// 8) 回归护栏：只加标签不加面板（修复前的坏状态）必须被 Go 侧校验拒绝
var bad = insert('core.tabs');
bad.node.props.tabs.push({ label: '页签9' });
cases.push(snapshot('labels-only-dirty', bad));

console.log(JSON.stringify({ cases: cases }));
`

// alignedEntryCount 解析节点 props[key] 数组长度。
func alignedEntryCount(t *testing.T, n *core.Node, key string) int {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(n.Props, &raw); err != nil {
		t.Fatalf("节点 %s props 解析失败: %v", n.ID, err)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw[key], &list); err != nil {
		t.Fatalf("节点 %s props.%s 解析失败: %v", n.ID, key, err)
	}
	return len(list)
}

// alignedLabels 解析 props[key] 数组里的文案字段（tabs→label，items→title）。
func alignedLabels(t *testing.T, n *core.Node, key, field string) []string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(n.Props, &raw); err != nil {
		t.Fatalf("节点 %s props 解析失败: %v", n.ID, err)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw[key], &list); err != nil {
		t.Fatalf("节点 %s props.%s 解析失败: %v", n.ID, key, err)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, _ := item[field].(string)
		out = append(out, s)
	}
	return out
}

// alignedSubtreeIDs 收集子树（含根）的 ID，返回是否存在重复。
func alignedSubtreeIDs(n *core.Node) (ids []string, dup string) {
	seen := map[string]bool{}
	var walk func(x *core.Node)
	walk = func(x *core.Node) {
		if x == nil {
			return
		}
		if seen[x.ID] {
			dup = x.ID
		}
		seen[x.ID] = true
		ids = append(ids, x.ID)
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return ids, dup
}

// sameStrings 顺序敏感比较。
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAlignedMutationKeepsLabelsAndChildrenInSync 核心不变量：
// 检查器的加/删/调序操作后，props 数组长度必须等于 children 数量，且节点仍可编译。
func TestAlignedMutationKeepsLabelsAndChildrenInSync(t *testing.T) {
	probe := runAlignedProbe(t)
	for _, c := range probe.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
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
			// labels-only-dirty 是故意构造的坏状态，校验必须失败（见下方分支）。
			if c.Name != "labels-only-dirty" {
				if err := core.ValidateNode(c.Node, map[string]bool{}); err != nil {
					t.Errorf("Validate 失败: %v", err)
				}
			}

			switch c.Name {
			case "tabs-add-1", "accordion-add-1":
				if goEntries != 2 || len(c.Node.Children) != 2 {
					t.Errorf("加一个标签后应为 2 个标签 / 2 个面板，实际 %d / %d", goEntries, len(c.Node.Children))
				}
				if c.KeptChildID != "" && !sameStrings([]string{childIDs[0]}, []string{c.KeptChildID}) {
					t.Errorf("原有面板被挤走：首个子节点 %s，期望 %s", childIDs[0], c.KeptChildID)
				}
			case "tabs-add-2":
				if goEntries != 3 || len(c.Node.Children) != 3 {
					t.Errorf("连续加两个标签后应为 3 / 3，实际 %d / %d", goEntries, len(c.Node.Children))
				}
				if len(childIDs) != 3 {
					t.Fatalf("子节点数量异常: %v", childIDs)
				}
				uniq := map[string]bool{}
				for _, id := range childIDs {
					if uniq[id] {
						t.Errorf("连续添加产生重复 ID: %s", id)
					}
					uniq[id] = true
				}
			case "tabs-remove-middle", "accordion-remove-first":
				if !sameStrings(childIDs, c.ExpectedChildIDs) {
					t.Errorf("删除后子节点顺序不符：%v，期望 %v", childIDs, c.ExpectedChildIDs)
				}
				for _, id := range childIDs {
					if id == c.RemovedChildID {
						t.Errorf("被删标签对应的子节点仍在文档中: %s", id)
					}
				}
			case "tabs-move-first-to-last":
				if !sameStrings(childIDs, c.ExpectedChildIDs) {
					t.Errorf("重排后子节点顺序不符：%v，期望 %v", childIDs, c.ExpectedChildIDs)
				}
				labels := alignedLabels(t, c.Node, c.Key, "label")
				if !sameStrings(labels, c.ExpectedLabels) {
					t.Errorf("重排后标签顺序不符：%v，期望 %v", labels, c.ExpectedLabels)
				}
			case "illegal-ops-return-null":
				if !c.IllegalAllNull {
					t.Error("非法操作（越界删除 / 原位移动 / 非结构型组件）没有返回 null")
				}
			case "labels-only-dirty":
				// 修复前的坏状态：Go 校验必须拦下（证明校验没有被削弱）。
				if goEntries == len(c.Node.Children) {
					t.Fatalf("只加标签不同步面板竟然数量一致（%d），护栏用例失效", goEntries)
				}
				err := core.ValidateNode(c.Node, map[string]bool{})
				if err == nil {
					t.Fatal("标签数与面板数不一致竟然通过校验（校验规则被削弱？）")
				}
				if !strings.Contains(err.Error(), "需与面板数量") {
					t.Errorf("报错信息不符: %v", err)
				}
			}
		})
	}
}

// TestAlignedMutationNodesCompile 每个操作序列的产物都必须能真正编译出 HTML。
func TestAlignedMutationNodesCompile(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	for _, c := range runAlignedProbe(t).Cases {
		c := c
		if c.Name == "labels-only-dirty" {
			continue // 故意的坏状态，编译必然失败
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
