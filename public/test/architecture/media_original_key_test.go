package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/public/test/support"
)

func TestNoPreserveNameInProduction(t *testing.T) {
	root := support.RepoRoot(t)
	var offenders []string

	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "pkg")} {
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {

				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "PreserveName" {
					return true
				}

				if v, ok := kv.Value.(*ast.Ident); ok && v.Name == "true" {
					if rel, rerr := filepath.Rel(root, path); rerr == nil {
						offenders = append(offenders, rel)
					}
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("扫描 %s 失败: %v", dir, walkErr)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("生产代码出现 PreserveName: true —— 存储 key 会采用用户原名，"+
			"/storage 的 immutable 判定随之不再安全（原图 URL 可能长得像带指纹的变体名，"+
			"换图是原地覆盖字节，老访客会长期看到旧图）。\n"+
			"若确实需要保留原名，必须同时改 internal/middleware/builtin/storage_cache.go "+
			"的判据（改成按记录判定而不是按文件名），不能只放开这个开关。\n命中文件: %v", offenders)
	}
}
