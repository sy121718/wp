// Package architecture 存放**跨模块边界**的架构测试。
//
// 这些测试不验证行为，只验证依赖方向：一条越界的 import 不会让任何用例变红，
// 却会让两个模块从此分不开 —— 而分不开的代价，要到下一次「只想改 A 却必须动 B」
// 的时候才显现。审计 CQ-003 / CQ-004 / CQ-005 / CQ-006 修的就是这一类问题。
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

// forbiddenCrossModuleDTO 已确立的跨模块 dto 边界：左侧模块的生产代码不得 import
// 右侧的 dto 包 —— 跨模块只经对方的 contract。
//
// 为什么逐条列出，而不是「一律禁止跨模块 dto」：仓库里还有若干**不在本次审计范围内**
// 的既有跨模块 dto 引用（product → presentation/dto、navigation → content/dto 等）。
// 一刀切会让本测试从第一天起就是红的，而一条长期红的红线等于没有红线。
// 清单只增不减：新增跨模块交互时，先决定要不要把它加进来。
var forbiddenCrossModuleDTO = []struct {
	from  string // 调用方模块目录（internal/module 下的一级目录）
	deny  string // 被禁的 import 前缀
	audit string // 依据
}{
	{
		from:  "cart",
		deny:  "go_wp/internal/module/order/dto",
		audit: "CQ-003：结算建单 / 支付落账经 ordercontract 的形状重导出",
	},
	{
		from:  "order",
		deny:  "go_wp/internal/module/product/inventory/dto",
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

// knownCrossModuleServiceModelDebt 通用规则的**已知存量偏差**（不是允许，是待还的债）。
//
// 这条规则（不得 import 其他模块的 service / model）是 AGENTS.md 的明文规定，
// 但在本次审计范围之外仍有存量违规。架构测试会把它们点出来，却不能把红线长期挂成红的 ——
// 一条长期失败的红线等于没有红线。因此逐条登记位置与依据，
// 让下一次改到那片代码的人顺手还掉；只增不减：新增偏差必须写进这张表才可能通过，
// 而写进这张表就意味着它会被读到。
var knownCrossModuleServiceModelDebt = []struct {
	from string
	deny string
	note string
}{
	{
		from: "presentation",
		deny: "go_wp/internal/module/contenttemplate/model",
		note: "presentation/service/presentation_archive.go 直接 import contenttemplate/model（应经 contenttemplate/contract）；审计 CQ 系列范围外，2026-09 由本测试检出",
	},
}

// knownDebt 判断某条 import 是否落在已知偏差表内。
func knownDebt(owner, importPath string) bool {
	for _, d := range knownCrossModuleServiceModelDebt {
		if d.from == owner && (importPath == d.deny || strings.HasPrefix(importPath, d.deny+"/")) {
			return true
		}
	}
	return false
}

// TestNoCrossModuleDTOImport 逐条断言上面那份边界清单。
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

// TestNoCrossModuleServiceModelImport 通用边界（AGENTS.md「命名约束」与
// internal/module/CLAUDE.md「表隔离约定」）：任何模块都不得 import 其他模块的
// service / model 包。
//
// product/inventory 视为 product 的**子模块**（issue #32：库存已并入商品域，业务层
// 同模块直调），因此 product/service → product/inventory/model 属同模块内调用，
// 不算越界 —— 这也正是审计 CQ-002 的核查结论。
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

// splitModuleImport 把 go_wp/internal/module/<x>/.../<pkg> 拆成（顶层模块, 末段包名）。
// 不在 internal/module 下的 import 返回 ok=false。
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

// scanImports 遍历 dir 下所有 .go 文件（含 _test.go），返回命中的「文件 → import」。
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

// parseImports 只解析 import 段（不解析函数体，代价可控）。
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

// repoRoot 从当前工作目录向上找 go.mod。
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

// repoRootFrom 从 dir 向上找含 go.mod 的目录；找不到返回空串。
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
