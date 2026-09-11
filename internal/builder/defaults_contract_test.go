// defaults_contract_test.go — 组件库「插入即合法」契约测试。
//
// 背景：从组件库拖入/点击插入的组件，如果只带 props 没有必需内容（子节点或数组项），
// 预览会立刻编译失败（如 core.tabs 报「页签至少需要一个面板」）。修复方向是
// 「插入时不留空壳」，而不是放宽校验。
//
// 默认内容只在 internal/templates/static/js/workbench/palette.js 声明一处，
// 本测试用 node 求值该模块的 buildInsertNode（与浏览器点一下组件同源），
// 再把真实产物交给 Go 侧 Validate / Compile —— 不存在手抄一份测试数据后
// 与前端漂移的问题。环境没有 node 时自动跳过。

package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// paletteJSRel palette.js 相对本包测试工作目录的路径。
const paletteJSRel = "../templates/static/js/workbench/palette.js"

// paletteProbe 前端组件库探针结果（node 求值 palette.js 的产物）。
type paletteProbe struct {
	// Nodes 每个组件库条目「插入后」的节点（buildInsertNode 输出）。
	Nodes []*core.Node `json:"nodes"`
	// ItemTypes paletteItems 的组件类型列表。
	ItemTypes []string `json:"itemTypes"`
	// GroupTypes paletteGroups 声明的组件类型列表（面板分组顺序）。
	GroupTypes []string `json:"groupTypes"`
}

// runPaletteProbe 用 node 求值前端 palette.js，返回组件库的真实插入产物与清单。
func runPaletteProbe(t *testing.T) (p paletteProbe) {
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
		t.Skip("未找到 node，跳过前端组件库契约测试")
	}
	// node 的 ESM 入口用 file:// URL，避免相对路径歧义。
	url := "file://" + filepath.ToSlash(abs)
	out, err := exec.Command(nodeBin, "--input-type=module", "--eval", fmt.Sprintf(paletteProbeScript, url)).CombinedOutput()
	if err != nil {
		t.Fatalf("node 求值 palette.js 失败: %v\n%s", err, out)
	}
	if err = json.Unmarshal(out, &p); err != nil {
		t.Fatalf("解析组件库探针 JSON 失败: %v\n%s", err, out)
	}
	if len(p.Nodes) == 0 {
		t.Fatal("组件库为空，palette.js 可能被改坏")
	}
	return p
}

// paletteInsertNodes 组件库每个条目「插入后」的节点。
func paletteInsertNodes(t *testing.T) []*core.Node {
	t.Helper()
	return runPaletteProbe(t).Nodes
}

// paletteProbeScript node 侧探针：构造插入节点并导出清单。
// 与浏览器里的插入路径同源（同一个 buildInsertNode）。
const paletteProbeScript = `
import { paletteItems, paletteGroups, buildInsertNode } from %q;
// 每次插入新建一个分配器（与浏览器里的 makeIdAllocator 同语义）。
function makeAlloc() {
  var seq = {};
  return function (base) { seq[base] = (seq[base] || 0) + 1; return base + '-' + seq[base]; };
}
var groupTypes = [];
paletteGroups.forEach(function (g) { (g.types || []).forEach(function (t) { groupTypes.push(t); }); });
console.log(JSON.stringify({
  nodes: paletteItems.map(function (item) { return buildInsertNode(item, makeAlloc()); }),
  itemTypes: paletteItems.map(function (item) { return item.type; }),
  groupTypes: groupTypes
}));
`

// structuralRequired A 类：Validate 要求至少一个子节点（空壳必然编译失败）。
var structuralRequired = map[string]string{
	"core.tabs":      "页签至少需要一个面板",
	"core.accordion": "手风琴至少需要一个折叠项",
	"core.slider":    "轮播至少需要一个 slide",
	"core.marquee":   "跑马灯至少需要一个内容",
}

// arrayPropsRequired B 类：Validate 要求 props 数组至少一项。
var arrayPropsRequired = map[string]string{
	"core.faq":     "常见问题至少需要一条",
	"core.form":    "表单至少需要一个字段",
	"core.gallery": "必须提供静态图集或 CMS 图集绑定",
}

// emptyNode 构造「空壳」节点：无 props、无 children。
func emptyNode(typeName string) *core.Node {
	return &core.Node{ID: "probe-1", Type: typeName, Props: json.RawMessage("{}")}
}

// TestPaletteGroupsCoverItems 组件库分组与条目必须互相覆盖：
// 分组里声明的类型必须在条目里存在（否则组件库看不到它 —— 历史上 core.gallery
// 就漏在 paletteItems 之外），每个条目也必须归入某个分组（否则拖不出来）。
func TestPaletteGroupsCoverItems(t *testing.T) {
	p := runPaletteProbe(t)
	items := map[string]bool{}
	for _, typ := range p.ItemTypes {
		items[typ] = true
	}
	grouped := map[string]bool{}
	for _, typ := range p.GroupTypes {
		grouped[typ] = true
		if !items[typ] {
			t.Errorf("分组声明了 %s，但组件库没有该条目（面板里看不到）", typ)
		}
	}
	for _, typ := range p.ItemTypes {
		if !grouped[typ] {
			t.Errorf("组件库条目 %s 没有归入任何分组（面板里拖不出来）", typ)
		}
	}
}

// TestPaletteInsertNodesValidate 组件库每个条目插入后都必须通过节点校验。
func TestPaletteInsertNodesValidate(t *testing.T) {
	for _, n := range paletteInsertNodes(t) {
		n := n
		t.Run(n.Type, func(t *testing.T) {
			if err := core.ValidateNode(n, map[string]bool{}); err != nil {
				t.Errorf("组件库条目 %s 插入后校验失败（插入即空壳）: %v", n.Type, err)
			}
		})
	}
}

// TestPaletteInsertNodeIDsUnique 每个条目插入出的子树内节点 ID 必须唯一：
// 默认子树里可能包含多个同类型子节点（未来扩展），同批分配器必须逐个记账。
func TestPaletteInsertNodeIDsUnique(t *testing.T) {
	for _, n := range paletteInsertNodes(t) {
		seen := map[string]bool{}
		var walk func(list []*core.Node)
		walk = func(list []*core.Node) {
			for _, c := range list {
				if seen[c.ID] {
					t.Errorf("%s 插入子树内 ID 重复: %s", n.Type, c.ID)
				}
				seen[c.ID] = true
				walk(c.Children)
			}
		}
		seen[n.ID] = true
		walk(n.Children)
	}
}

// TestPaletteInsertNodesFillRequiredContent 逐个受影响组件验证：
// 空壳必须被拒绝（证明校验仍在），插入产物必须通过且内容可见可编辑。
func TestPaletteInsertNodesFillRequiredContent(t *testing.T) {
	byType := map[string]*core.Node{}
	for _, n := range paletteInsertNodes(t) {
		byType[n.Type] = n
	}

	t.Run("空壳必须被拒绝", func(t *testing.T) {
		for typ, wantMsg := range structuralRequired {
			err := core.ValidateNode(emptyNode(typ), map[string]bool{})
			if err == nil {
				t.Errorf("%s 空壳节点竟然通过校验（校验规则被削弱？）", typ)
				continue
			}
			if !strings.Contains(err.Error(), wantMsg) {
				t.Errorf("%s 空壳报错信息不符: %v", typ, err)
			}
		}
		for typ, wantMsg := range arrayPropsRequired {
			err := core.ValidateNode(emptyNode(typ), map[string]bool{})
			if err == nil {
				t.Errorf("%s 空壳节点竟然通过校验（校验规则被削弱？）", typ)
				continue
			}
			if !strings.Contains(err.Error(), wantMsg) {
				t.Errorf("%s 空壳报错信息不符: %v", typ, err)
			}
		}
	})

	t.Run("插入产物必须通过校验", func(t *testing.T) {
		for _, typ := range append(keysOf(structuralRequired), keysOf(arrayPropsRequired)...) {
			n := byType[typ]
			if n == nil {
				t.Errorf("%s 不在组件库中", typ)
				continue
			}
			if err := core.ValidateNode(n, map[string]bool{}); err != nil {
				t.Errorf("%s 插入后校验失败: %v", typ, err)
			}
		}
	})

	t.Run("默认子节点可见可编辑", func(t *testing.T) {
		for typ := range structuralRequired {
			n := byType[typ]
			if n == nil {
				continue
			}
			if len(n.Children) == 0 {
				t.Errorf("%s 插入后仍然没有子节点", typ)
				continue
			}
			visibleText := 0
			var walk func(list []*core.Node)
			walk = func(list []*core.Node) {
				for _, c := range list {
					if c.Hidden || c.Locked {
						t.Errorf("%s 默认子节点 %s 被隐藏/锁定，画布与大纲看不到", typ, c.ID)
					}
					if c.Type == "core.text" || c.Type == "core.heading" {
						var p struct {
							Text string `json:"text"`
						}
						_ = json.Unmarshal(c.Props, &p)
						if p.Text != "" {
							visibleText++
						}
					}
					walk(c.Children)
				}
			}
			walk(n.Children)
			if visibleText == 0 {
				t.Errorf("%s 默认子树里没有可编辑的文本内容（画布上无内容可点）", typ)
			}
		}
	})

	t.Run("内容数组与子节点数量一致", func(t *testing.T) {
		// tabs/accordion 的 props 数组必须与 children 一一对应，否则同样编译失败。
		cases := map[string]string{"core.tabs": "tabs", "core.accordion": "items"}
		for typ, key := range cases {
			n := byType[typ]
			if n == nil {
				continue
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(n.Props, &raw); err != nil {
				t.Fatalf("%s props 解析失败: %v", typ, err)
			}
			var list []json.RawMessage
			if err := json.Unmarshal(raw[key], &list); err != nil {
				t.Fatalf("%s props.%s 解析失败: %v", typ, key, err)
			}
			if len(list) != len(n.Children) {
				t.Errorf("%s props.%s 数量 %d 与子节点数量 %d 不一致", typ, key, len(list), len(n.Children))
			}
		}
	})
}

// paletteContentResolver 组件库编译用例的桩解析器：任何字段返回固定占位值。
//
// 为什么需要它：core.product（商品详情，issue #6）声明的槽位全是数据绑定，
// 没有内容解析器就编译不出来 —— 而真实构建（发布实例路径）必注入解析器。
// 这里只验证「组件库条目插入后编译得出来」，字段语义由 product 模块测试覆盖，
// 故桩实现按「有值」返回，不校验白名单（白名单校验有独立用例）。
type paletteContentResolver struct{}

// ResolveString 实现 core.ContentResolver。
func (paletteContentResolver) ResolveString(string) (string, error) { return "示例值", nil }

// TestPaletteInsertNodesCompile 组件库每个条目插入后都必须能真正编译出产物。
func TestPaletteInsertNodesCompile(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	for _, n := range paletteInsertNodes(t) {
		n := n
		// 每个组件一条「拖入即编译成功」用例（子测试名 = 组件类型）。
		t.Run(n.Type, func(t *testing.T) {
			// 页面设置需给出合法版心模式（与工作台默认文档一致），否则编译在校验前就报错。
			doc, err := json.Marshal(map[string]any{
				"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
				"root":     []*core.Node{n},
			})
			if err != nil {
				t.Fatalf("序列化文档失败: %v", err)
			}
			page, err := ParsePage(doc)
			if err != nil {
				t.Fatalf("%s 文档解析失败: %v", n.Type, err)
			}
			if _, err = Compile(page, WithComponentSet(set), WithContentResolver(paletteContentResolver{})); err != nil {
				t.Errorf("%s 插入后编译失败: %v", n.Type, err)
			}
		})
	}
}

// keysOf 取 map 键（保证确定性顺序：按 key 排序后返回）。
func keysOf(m map[string]string) (out []string) {
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
