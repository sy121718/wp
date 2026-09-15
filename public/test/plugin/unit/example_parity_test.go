package unit

// 示例插件一致性验收（OSS-016）：examples/l0-demo 的安装相关文件与
// scaffold.Files("l0demo") 产物逐字节一致 —— 保证「scaffold 生成即可安装」
// 与「示例可安装」是同一条链路；README 属说明文档不参与比对。

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go_wp/internal/module/plugin/scaffold"
)

// repoRoot 从测试文件位置回推仓库根（../../../.. 到 go_wp/）。
func repoRoot(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	// public/test/plugin/unit/example_parity_test.go → 仓库根
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(abs)))))
}

// TestExamplePluginParity 示例插件与 scaffold 产物一致（安装相关文件）。
func TestExamplePluginParity(t *testing.T) {
	want, err := scaffold.Files("l0demo")
	if err != nil {
		t.Fatalf("scaffold.Files: %v", err)
	}
	exampleRoot := filepath.Join(repoRoot(t), "examples", "l0-demo")
	for rel, wantContent := range want {
		if rel == "README.md" {
			// README 属人读说明（含能力缺口标注），不参与逐字节比对。
			continue
		}
		got, err := os.ReadFile(filepath.Join(exampleRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("示例缺文件 %s: %v", rel, err)
		}
		if string(got) != wantContent {
			t.Errorf("示例文件 %s 与 scaffold 产物不一致（请用 scaffold 重新生成或同步修改）", rel)
		}
	}
}

// TestExamplePluginInstallable 示例插件走真实 Install → EnabledAssembly 全链。
func TestExamplePluginInstallable(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	exampleRoot := filepath.Join(repoRoot(t), "examples", "l0-demo")
	files := map[string]string{}
	for _, rel := range []string{"manifest.json", "components/l0demo_card.jet", "migrations/001_init.sql"} {
		b, err := os.ReadFile(filepath.Join(exampleRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("读示例文件 %s: %v", rel, err)
		}
		files[rel] = string(b)
	}
	zipBytes := zipFiles(t, files)
	if _, err := svc.Install(context.Background(), zipBytes); err != nil {
		t.Fatalf("示例插件安装失败: %v", err)
	}
	asm, err := svc.EnabledAssembly(context.Background())
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	if _, ok := asm.Specs["plugin.l0demo.l0demo_card"]; !ok {
		t.Fatalf("缺组件规格 plugin.l0demo.l0demo_card，规格数=%d", len(asm.Specs))
	}
}
