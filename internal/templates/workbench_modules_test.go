package templates

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 按浏览器的 ES module 语法解析整个工作台，不能依赖 Node 对 .js 的模式猜测。
// 只测 palette 纯函数或 Go 返回的 HTML，无法发现未加载模块里的语法错误。
func TestWorkbenchModuleSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("工作台检查要求 Node，请通过 vfox 准备测试运行时")
		}
		t.Skip("缺少 Node；完整工作台检查请运行 scripts/check-workbench.sh")
	}
	err = filepath.WalkDir("static/js/workbench", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, "--input-type=module", "--check")
			cmd.Stdin = strings.NewReader(string(data))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("ES module 解析失败: %v\n%s", err, out)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 加载实际控件入口，额外拦截「语法合法，但 import/export 名称错位」。
	entry, err := filepath.Abs("static/js/workbench/methods/controls/misc.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "--eval", "await import(process.argv[1])", entry)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("控件模块依赖图加载失败: %v\n%s", err, out)
	}
}
