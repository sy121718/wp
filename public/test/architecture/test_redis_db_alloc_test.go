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

// TestRedisFixtureConnectsToAllocatedDB 钉住「分配表 → 实际连接的 DB」这条链：
// 分配表算对了号但没传进 cfg（或 redis.db 键名写错）时，隔离静默失效 —— 本测试会红。
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

// TestTestRedisDBAllocationMatchesCallers 对账「测试包 → Redis DB 号」分配表与真实调用点。
//
// 背景：会话 key 是 user:session:<userID>（pkg/auth/session.go:66），各 feature 测试包的管理员
// 在各自独立的 PG 测试库里 userID 恒为 1 —— 共用 Redis DB 时一个包的登出会删掉另一个包的会话，
// 表现为并发全量偶发 302 /admin/login 的假红（与 Go 版本无关，见 public/test/support/admin_session.go）。
//
// 三个方向都判失败：
//  1. 有调用点但未登记（新包会静默退回 DB 0，缺陷复发）
//  2. 登记了但已无调用点（陈旧条目，登记表退化成注释）
//  3. 号重复或占用 DB 0（隔离失效）
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

// parseRedisDBAlloc 用 AST 读 testRedisDBByDir 的字面量，避免与实现漂移。
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

// scanRedisFixtureCallers 扫出所有以 support.SetupRedisForTest* 调用发起方的仓库相对目录。
func scanRedisFixtureCallers(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
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

// supportImportAlias 返回本文件里 go_wp/public/test/support 的本地引用名（无则空串）。
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

// repoRelDir 把绝对目录折成相对仓库根的斜杠路径。
func repoRelDir(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}
