package feature

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"

	"golang.org/x/net/html"
)

// 从真实 HTTP 片段提取属性，再驱动真实 JS 绑定函数，输出交回 Go 编译。
// Node 探针只模拟事件委托接口，不模拟浏览器布局；多端和真实输入另做浏览器验证。
func TestInspectorRepeaterClientContract(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("完整工作台检查要求 Node")
		}
		t.Skip("缺少 Node；运行 scripts/check-workbench.sh 完整检查")
	}
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"core.tabs", "core.accordion"} {
		t.Run(typ, func(t *testing.T) {
			spec := core.AlignedRepeaterFor(typ)
			props, _ := json.Marshal(map[string]any{spec.AlignKey: []map[string]any{
				{spec.Field: "第一项"}, {spec.Field: "第二项"},
			}})
			n := &core.Node{ID: "rep1", Type: typ, Props: props, Children: []*core.Node{
				{ID: "p1", Type: "core.text", Props: json.RawMessage(`{"text":"内容一"}`)},
				{ID: "p2", Type: "core.text", Props: json.RawMessage(`{"text":"内容二"}`)},
			}}
			doc, _ := json.Marshal(map[string]any{"settings": map[string]any{"layout": map[string]any{"mode": "full"}}, "root": []*core.Node{n}})
			body := postInspector(t, string(doc), n.ID, "content")
			parsed, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]string
			var elements []map[string]string
			var walk func(*html.Node, bool)
			walk = func(n *html.Node, inside bool) {
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				if attrs["data-wb-rep"] != "" {
					root, inside = attrs, true
				}
				if inside && (n.Data == "input" || n.Data == "button") {
					elements = append(elements, attrs)
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, inside)
				}
			}
			walk(parsed, false)
			payload, _ := json.Marshal(map[string]any{"node": n, "root": root, "elements": elements})
			probe, _ := filepath.Abs("../fixtures/repeater_probe.mjs")
			cmd := exec.Command(nodeBin, probe)
			cmd.Stdin = bytes.NewReader(payload)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("真实片段到 JS 绑定失败: %v\n%s", err, out)
			}
			var results []struct {
				Name string     `json:"name"`
				Node *core.Node `json:"node"`
			}
			if err := json.Unmarshal(out, &results); err != nil || len(results) < 5 {
				t.Fatalf("探针结果不完整: %v\n%s", err, out)
			}
			for _, result := range results {
				t.Run(result.Name, func(t *testing.T) {
					if err := core.ValidateNode(result.Node, map[string]bool{}); err != nil {
						t.Fatal(err)
					}
					doc, _ := json.Marshal(map[string]any{"settings": map[string]any{"layout": map[string]any{"mode": "full"}}, "root": []*core.Node{result.Node}})
					page, err := builder.ParsePage(doc)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := builder.Compile(page, builder.WithComponentSet(set)); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
