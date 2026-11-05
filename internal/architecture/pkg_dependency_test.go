package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 依赖方向判据：pkg 只能依赖标准库、第三方与本仓其它 pkg 包。
// pkg 一旦 import internal/config/public/migrations，框架就不再可独立使用。
var pkgForbiddenImports = []string{
	"go_wp/internal/",
	"go_wp/config/",
	"go_wp/public/",
}

// 额外的结构性判据：UI 不依赖构建器，发布内核不依赖编译实现，Jet 不依赖构建器。
var pkgStructuralRules = []struct {
	from    string
	forbid  []string
	message string
}{
	{
		from:    "pkg/view/ui",
		forbid:  []string{"go_wp/pkg/builder", "go_wp/internal/"},
		message: "公共 UI 不得依赖构建器或 internal",
	},
	{
		from:    "pkg/view/jet",
		forbid:  []string{"go_wp/pkg/builder", "go_wp/internal/"},
		message: "Jet 适配层不得依赖构建器或 internal",
	},
	{
		from:    "pkg/publish",
		forbid:  []string{"go_wp/pkg/builder", "go_wp/internal/"},
		message: "发布内核不得依赖编译实现或 internal",
	},
}

// TestPkgDoesNotImportInternal 保证 pkg 层对上层零依赖。
func TestPkgDoesNotImportInternal(t *testing.T) {
	root := repoRoot(t)
	pkgDir := filepath.Join(root, "pkg")

	var offenders []string
	err := filepath.WalkDir(pkgDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		imports := fileImports(t, path)
		for _, imp := range imports {
			for _, forbidden := range pkgForbiddenImports {
				if strings.HasPrefix(imp, forbidden) {
					rel := mustRel(t, root, path)
					offenders = append(offenders, rel+" -> "+imp)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 pkg 失败：%v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("pkg 不得 import internal/config/public：\n%s", strings.Join(offenders, "\n"))
	}
}

// TestPkgStructuralBoundaries 守住 view/ui、jet、publish 的依赖方向。
func TestPkgStructuralBoundaries(t *testing.T) {
	root := repoRoot(t)
	for _, rule := range pkgStructuralRules {
		dir := filepath.Join(root, rule.from)
		if _, err := os.Stat(dir); err != nil {
			// 目录尚未建立时跳过：本判据用于防止阶段迁移中引入反向依赖。
			continue
		}
		var offenders []string
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			for _, imp := range fileImports(t, path) {
				for _, forbidden := range rule.forbid {
					if strings.HasPrefix(imp, forbidden) {
						offenders = append(offenders, mustRel(t, root, path)+" -> "+imp)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", rule.from, err)
		}
		if len(offenders) > 0 {
			t.Fatalf("%s：\n%s", rule.message, strings.Join(offenders, "\n"))
		}
	}
}

// fileImports 解析单个 Go 文件的 import 路径。
func fileImports(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", path, err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		v, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("解引号 %s 失败：%v", path, err)
		}
		out = append(out, v)
	}
	return out
}

// mustRel 返回相对仓库根的路径，失败即终止测试。
func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("取相对路径失败 %s：%v", path, err)
	}
	return rel
}
