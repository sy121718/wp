package architecture

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/cache"
	"go_wp/public/test/support"
)

func TestRedisFixtureConnectsToAllocatedDB(t *testing.T) {
	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("Redis 不可用，跳过: %v", err)
	}
	client, err := cache.GetRedis()
	if err != nil {
		t.Fatalf("取 Redis 客户端失败: %v", err)
	}
	info, err := client.Do(context.Background(), "CLIENT", "INFO").Text()
	if err != nil {
		t.Fatalf("CLIENT INFO 失败: %v", err)
	}
	if !strings.Contains(info, "db=5") {
		t.Fatalf("测试连接未落在分配表登记的 DB 5：%s", firstLineWith(info, "db="))
	}
}

func firstLineWith(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return "(未找到 db= 字段)"
}

func TestTestRedisDBAllocationMatchesCallers(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	allocFile := filepath.Join(root, "public", "test", "support", "admin_session.go")
	alloc := parseRedisDBAlloc(t, allocFile)

	callers := scanRedisFixtureCallers(t, root)

	for dir := range callers {
		if _, ok := alloc[dir]; !ok {
			t.Errorf("调用点目录未登记 Redis DB：%s（请在 testRedisDBByDir 领号）", dir)
		}
	}
	for dir := range alloc {
		if _, ok := callers[dir]; !ok {
			t.Errorf("登记表条目已无调用点：%s（请删除该条目）", dir)
		}
	}

	seen := map[int]string{}
	for dir, db := range alloc {
		if db == 0 {
			t.Errorf("测试包 %s 占用了 DB 0（生产默认值），请换 1..15", dir)
		}
		if prev, dup := seen[db]; dup {
			t.Errorf("DB %d 被两个测试包共用：%s 与 %s（隔离失效）", db, prev, dir)
		}
		seen[db] = dir
	}
}

func parseRedisDBAlloc(t *testing.T, path string) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	out := map[string]int{}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != "testRedisDBByDir" || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			found = true
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.BasicLit)
				if !ok {
					continue
				}
				dir, err := strconv.Unquote(key.Value)
				if err != nil {
					continue
				}
				val, ok := kv.Value.(*ast.BasicLit)
				if !ok {
					continue
				}
				db, err := strconv.Atoi(val.Value)
				if err != nil {
					continue
				}
				out[dir] = db
			}
		}
		return true
	})
	if !found {
		t.Fatalf("%s 中未找到 testRedisDBByDir 字面量", path)
	}
	return out
}

func scanRedisFixtureCallers(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {

			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == ".worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		alias := supportImportAlias(f)
		if alias == "" {
			return nil
		}
		used := false
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias && strings.HasPrefix(sel.Sel.Name, "SetupRedisForTest") {
				used = true
			}
			return true
		})
		if used {
			out[repoRelDir(root, filepath.Dir(p))] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描调用点失败: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("未扫描到任何 SetupRedisForTest 调用点，门禁失效")
	}
	return out
}

func supportImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != "go_wp/public/test/support" {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
		return "support"
	}
	return ""
}

func repoRelDir(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}
