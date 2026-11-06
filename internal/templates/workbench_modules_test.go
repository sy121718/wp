package templates

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchModuleSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("工作台检查要求 Node，请通过 vfox 准备测试运行时")
		}
		t.Skip("缺少 Node；完整工作台检查请运行 scripts/check-workbench.sh")
	}
	assets, aerr := StaticAssets()
	if aerr != nil {
		t.Fatal(aerr)
	}
	for _, path := range assets.Keys() {
		if !strings.HasPrefix(path, "js/editor/") || !strings.HasSuffix(path, ".js") {
			continue
		}
		path := path
		t.Run(path, func(t *testing.T) {
			data, err := assets.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			raw, err := io.ReadAll(data)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, "--input-type=module", "--check")
			cmd.Stdin = strings.NewReader(string(raw))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("ES module 解析失败: %v\n%s", err, out)
			}
		})
	}

	entry, err := filepath.Abs("../../pkg/builder/editor/assets/js/editor/methods/controls/misc.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "--eval", "await import(process.argv[1])", entry)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("控件模块依赖图加载失败: %v\n%s", err, out)
	}
}
