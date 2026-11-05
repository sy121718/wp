package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
)

const paletteJSRel = "../templates/static/js/workbench/palette.js"

type paletteProbe struct {
	Nodes []*core.Node `json:"nodes"`

	ItemTypes []string `json:"itemTypes"`

	GroupTypes []string `json:"groupTypes"`
}

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

func paletteInsertNodes(t *testing.T) []*core.Node {
	t.Helper()
	return runPaletteProbe(t).Nodes
}

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

var structuralRequired = map[string]string{
	"core.tabs":      "页签至少需要一个面板",
	"core.accordion": "手风琴至少需要一个折叠项",
	"core.slider":    "轮播至少需要一个 slide",
	"core.marquee":   "跑马灯至少需要一个内容",
}

var arrayPropsRequired = map[string]string{
	"core.faq":     "常见问题至少需要一条",
	"core.form":    "表单至少需要一个字段",
	"core.gallery": "必须提供静态图集或 CMS 图集绑定",
}

func emptyNode(typeName string) *core.Node {
	return &core.Node{ID: "probe-1", Type: typeName, Props: json.RawMessage("{}")}
}

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

type paletteContentResolver struct{}

func (paletteContentResolver) ResolveString(string) (string, error) { return "示例值", nil }

func TestPaletteInsertNodesCompile(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	for _, n := range paletteInsertNodes(t) {
		n := n

		t.Run(n.Type, func(t *testing.T) {

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
			opts := []CompileOption{WithComponentSet(set), WithContentResolver(paletteContentResolver{})}
			if n.Type == "core.addToCart" {
				opts = append(opts, WithCompileMode(CompileModePreview))
			}
			if _, err = Compile(page, opts...); err != nil {
				t.Errorf("%s 插入后编译失败: %v", n.Type, err)
			}
		})
	}
}

func keysOf(m map[string]string) (out []string) {
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
