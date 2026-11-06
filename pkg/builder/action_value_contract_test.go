package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go_wp/pkg/builder/core"
)

const coreJSRel = "editor/assets/js/editor/core.js"

type actionValueCase struct {
	Action string `json:"action"`
	From   string `json:"from"`
	Value  string `json:"value"`
	Hint   string `json:"hint"`
}

type actionValuePatch struct {
	Path    string `json:"path"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
}

type actionValueProbe struct {
	Cases    []actionValueCase  `json:"cases"`
	Patch    []actionValuePatch `json:"patch"`
	PatchNil bool               `json:"patchNil"`
}

func buttonActionOptions(t *testing.T) []string {
	t.Helper()
	schemas, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("生成组件 schema 失败: %v", err)
	}
	raw, ok := schemas["core.button"]
	if !ok {
		t.Fatal("schema 里没有 core.button")
	}
	var items []struct {
		Key     string `json:"key"`
		Kind    string `json:"kind"`
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
	}
	if err = json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("解析 core.button schema 失败: %v", err)
	}
	for _, it := range items {
		if it.Key != "action" {
			continue
		}
		out := make([]string, 0, len(it.Options))
		for _, o := range it.Options {
			out = append(out, o.Value)
		}
		return out
	}
	t.Fatal("core.button 的 action 控件没有声明选项（ct tag 被改坏？）")
	return nil
}

func runActionValueProbe(t *testing.T, actions []string) actionValueProbe {
	t.Helper()
	abs, err := filepath.Abs(coreJSRel)
	if err != nil {
		t.Fatalf("解析 core.js 路径失败: %v", err)
	}
	if _, err = os.Stat(abs); err != nil {
		t.Fatalf("core.js 不存在: %v", err)
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未找到 node，跳过动作/值联动契约测试")
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		t.Fatalf("序列化动作清单失败: %v", err)
	}
	url := "file://" + filepath.ToSlash(abs)
	script := fmt.Sprintf(actionValueProbeScript, url, string(actionsJSON))
	out, err := exec.Command(nodeBin, "--input-type=module", "--eval", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node 求值 wbActionValue 失败: %v\n%s", err, out)
	}
	var p actionValueProbe
	if err = json.Unmarshal(out, &p); err != nil {
		t.Fatalf("解析联动探针 JSON 失败: %v\n%s", err, out)
	}
	return p
}

func TestActionValueLinkKeepsButtonValid(t *testing.T) {
	actions := buttonActionOptions(t)
	if len(actions) == 0 {
		t.Fatal("core.button 没有 action 选项")
	}
	p := runActionValueProbe(t, actions)
	if len(p.Cases) == 0 {
		t.Fatal("联动探针没有产出任何场景")
	}
	covered := map[string]bool{}
	for _, c := range p.Cases {
		c := c
		t.Run(c.Action+"<-"+c.From, func(t *testing.T) {
			covered[c.Action] = true
			node := &core.Node{
				ID:   "probe-1",
				Type: "core.button",
				Props: json.RawMessage(fmt.Sprintf(
					`{"text":"了解更多","action":%q,"value":%q}`, c.Action, c.Value)),
			}
			err := core.ValidateNode(node, map[string]bool{})
			if c.Hint == `` {
				if err != nil {
					t.Errorf("联动产物必须合法但校验失败: value=%q err=%v", c.Value, err)
				}
				return
			}

			if err == nil {
				t.Errorf("带提示的值竟然通过校验（提示失真）: value=%q hint=%q", c.Value, c.Hint)
			}
		})
	}
	for _, a := range actions {
		if !covered[a] {
			t.Errorf("动作 %s 没有联动场景（新增动作必须接入 WB_FIELD_LINKS）", a)
		}
	}
}

func TestActionValueLinkDeclaredAndChanged(t *testing.T) {
	p := runActionValueProbe(t, buttonActionOptions(t))
	if p.PatchNil {
		t.Fatal("core.button 的 action 没有声明字段联动（WB_FIELD_LINKS 缺失）")
	}
	if len(p.Patch) != 1 {
		t.Fatalf("联动条目数不符: %v", p.Patch)
	}
	got := p.Patch[0]
	if got.Path != "props.value" {
		t.Errorf("联动目标不是 props.value: %s", got.Path)
	}
	if !got.Changed {
		t.Errorf("internal+/shop 切成 native 后 value 未被改写（报障场景未修复）: %+v", got)
	}
	if got.Value != "tel:" {
		t.Errorf("native 的合法前缀骨架不符: got %q want %q", got.Value, "tel:")
	}
}

const actionValueProbeScript = `
globalThis.document = { getElementById: () => null, addEventListener: () => {}, querySelectorAll: () => [] };
const m = await import(%q);
const actions = %s;
const cases = [];
// 1) 每个 ct tag 声明的动作，都从「站内路径 /shop」切换过去（报障场景的旧值形态）
actions.forEach(function (a) {
  const r = m.wbActionValue(a, '/shop');
  cases.push({ action: a, from: '/shop', value: r.value, hint: r.hint });
});
// 2) 旧值本身可推导的场景：联动后必须直接合法
[['native', 'tel:13800000000'], ['native', '13800000000'], ['native', 'hi@example.com'],
 ['external', 'example.com'], ['external', 'https://a.com/x?y=1'], ['anchor', '#contact'],
 ['anchor', '/contact'], ['internal', 'https://a.com/x'], ['internal', ''], ['modal', '/dlg']
].forEach(function (pair) {
  const r = m.wbActionValue(pair[0], pair[1]);
  cases.push({ action: pair[0], from: pair[1], value: r.value, hint: r.hint });
});
// 3) 报障场景的联动改写
const node = { type: 'core.button', props: { action: 'native', value: '/shop' } };
const patch = m.wbLinkedPatch(node, 'props.action');
console.log(JSON.stringify({ cases: cases, patch: patch, patchNil: patch === null }));
`
