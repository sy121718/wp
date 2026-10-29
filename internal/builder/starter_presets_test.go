package builder

import (
	"os/exec"
	"path/filepath"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

// 编译界面中真实提供的区块，校验响应式值、动效和基础节点结构。
func TestStarterPresetsCompile(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("缺少 Node")
	}
	path, err := filepath.Abs("../templates/static/js/workbench/starter-presets.js")
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, "--input-type=module", "--eval", "const m=await import(process.argv[1]);console.log(JSON.stringify(m.starterPresets));", path).CombinedOutput()
	if err != nil {
		t.Fatalf("读取区块失败: %v\n%s", err, output)
	}
	var presets []struct {
		ID       string       `json:"id"`
		Document []*core.Node `json:"document"`
	}
	if err = json.Unmarshal(output, &presets); err != nil {
		t.Fatal(err)
	}
	if len(presets) == 0 {
		t.Fatal("组合区块不可为空")
	}
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range presets {
		t.Run(preset.ID, func(t *testing.T) {
			document, err := json.Marshal(map[string]any{"settings": map[string]any{"layout": map[string]any{"mode": "full"}}, "root": preset.Document})
			if err != nil {
				t.Fatal(err)
			}
			page, err := ParsePage(document)
			if err != nil {
				t.Fatal(err)
			}
			first, err := Compile(page, WithComponentSet(set))
			if err != nil {
				t.Fatal(err)
			}
			second, err := Compile(page, WithComponentSet(set))
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(second)
			if string(a) != string(b) {
				t.Fatal("相同区块编译结果不确定")
			}
		})
	}
}
