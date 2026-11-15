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

var forbiddenCrossModuleDTO = []struct {
	from  string
	deny  string
	audit string
}{
	{
		from:  "cart",
		deny:  "go_wp/internal/module/order/dto",
		audit: "CQ-003：结算建单 / 支付落账经 ordercontract 的形状重导出",
	},
	{
		from:  "order",
		deny:  "go_wp/internal/module/inventory/dto",
		audit: "CQ-004：库存入参用 ordercontract 自有类型（适配器在库存侧）",
	},
	{
		from:  "order",
		deny:  "go_wp/internal/module/user/dto",
		audit: "CQ-004：访客开号经 usercontract.GuestAccountInput",
	},
	{
		from:  "page",
		deny:  "go_wp/internal/module/media/dto",
		audit: "CQ-005：媒体引用同步经 mediacontract.SyncRefsInput",
	},
	{
		from:  "page",
		deny:  "go_wp/internal/module/publication/dto",
		audit: "CQ-005：发布回执形状经 pubcontract 重导出",
	},
	{
		from:  "user",
		deny:  "go_wp/internal/module/mail/dto",
		audit: "CQ-006：事务发信经 mailcontract.SendInput",
	},
}

var knownCrossModuleServiceModelDebt = []struct {
	from string
	deny string
	note string
}{
	{
		from: "presentation",
		deny: "go_wp/internal/module/contenttemplate/model",
		note: "presentation/service/presentation_instance.go 直接 import contenttemplate/model（应经 contenttemplate/contract）；审计 CQ 系列范围外，2026-09 由本测试检出",
	},
}

func knownDebt(owner, importPath string) bool {
	for _, d := range knownCrossModuleServiceModelDebt {
		if d.from == owner && (importPath == d.deny || strings.HasPrefix(importPath, d.deny+"/")) {
			return true
		}
	}
	return false
}

func TestNoCrossModuleDTOImport(t *testing.T) {
	root := repoRoot(t)
	for _, rule := range forbiddenCrossModuleDTO {
		t.Run(rule.from+"->"+strings.TrimPrefix(rule.deny, "go_wp/internal/module/"), func(t *testing.T) {
			dir := filepath.Join(root, "internal", "module", filepath.FromSlash(rule.from))
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("模块目录不存在（模块改名后请同步本测试清单）：%s", dir)
			}
			hits, err := scanImports(dir, func(importPath string) bool {
				return importPath == rule.deny || strings.HasPrefix(importPath, rule.deny+"/")
			})
			if err != nil {
				t.Fatalf("扫描 %s 失败：%v", rule.from, err)
			}
			if len(hits) > 0 {
				t.Fatalf("%s 模块绕过了契约层（%s）：%s",
					rule.from, rule.audit, strings.Join(hits, " | "))
			}
		})
	}
}

func TestNoCrossModuleServiceModelImport(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "internal", "module")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("读取模块目录失败：%v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		owner := e.Name()
		dir := filepath.Join(base, owner)
		hits, serr := scanImports(dir, func(importPath string) bool {
			if knownDebt(owner, importPath) {
				return false
			}
			mod, pkg, ok := splitModuleImport(importPath)
			if !ok || mod == owner {
				return false
			}
			return pkg == "service" || pkg == "model"
		})
		if serr != nil {
			t.Fatalf("扫描 %s 失败：%v", owner, serr)
		}
		if len(hits) > 0 {
			t.Errorf("%s 模块直接依赖了其他模块的 service / model（跨模块只允许 contract）：%s",
				owner, strings.Join(hits, " | "))
		}
	}
}

func splitModuleImport(importPath string) (module string, lastSeg string, ok bool) {
	const prefix = "go_wp/internal/module/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(importPath, prefix)
	segs := strings.Split(rest, "/")
	if len(segs) < 2 {
		return "", "", false
	}
	return segs[0], segs[len(segs)-1], true
}

func scanImports(dir string, match func(importPath string) bool) (hits []string, err error) {
	root := repoRootFrom(dir)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		imports, perr := parseImports(path)
		if perr != nil {
			return perr
		}
		for _, imp := range imports {
			if match(imp) {
				rel := path
				if root != "" {
					if r, rerr := filepath.Rel(root, path); rerr == nil {
						rel = r
					}
				}
				hits = append(hits, rel+" -> "+imp)
			}
		}
		return nil
	})
	return hits, err
}

func parseImports(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		value, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil {
			return nil, uerr
		}
		out = append(out, value)
	}
	return out, nil
}

func skipArchScanDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".worktrees":
		return true
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败：%v", err)
	}
	root := repoRootFrom(dir)
	if root == "" {
		t.Fatalf("从 %s 向上没有找到 go.mod", dir)
	}
	return root
}

func repoRootFrom(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
