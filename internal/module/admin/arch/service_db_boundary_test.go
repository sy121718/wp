// Package arch — admin 模块架构守卫（CQ-025）。
//
// 豁免理由（YG 拍板）：admin 模块是**内部控制面**，只服务内部运营人员，不是面向
// 外部访客的 API。项目整体约定「访问面零查库」针对的是公开访问路径；admin 的
// service 层直查 DB 不在迁移范围内，属于**已知且接受的偏差**。
//
// 本测试把所有 service 层直查 DB 的位置固化为「已知偏差表」，规则是**只增不减**：
//   - 代码里新增一处直查而表里没有 → 测试变红（必须显式登记，防止偏差静默扩散）；
//   - 修复删掉一处直查 → 测试同样变红（提示把该行从表里移除，保持账实一致）。
package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gormOps 判定为「gorm 链式 DB 操作」的方法名集合。
var gormOps = map[string]bool{
	"Where": true, "OrWhere": true, "Not": true,
	"Find": true, "First": true, "Take": true, "Last": true,
	"Scan": true, "Pluck": true, "Count": true,
	"Create": true, "CreateInBatches": true, "Save": true,
	"Delete": true, "Updates": true, "Update": true,
	"UpdateColumn": true, "UpdateColumns": true,
	"Model": true, "Preload": true, "Joins": true, "Association": true,
	"Raw": true, "Exec": true, "Row": true, "Rows": true,
	"Assign": true, "Attrs": true, "FirstOrCreate": true, "FirstOrInit": true,
}

// serviceRoot 被扫描的 service 层目录（相对本测试文件）。
const serviceRoot = "../service"

// scanServiceDBCalls 扫描 service 层所有 *gorm.DB 链式操作调用点。
//
// 判定：receiver 链中含 .DB(ctx) 调用，或根标识符在本函数内被赋值为 DB 会话
// （query := s.am.DB(ctx)... / func(tx *gorm.DB) 参数）。函数级近似，宁多勿漏。
func scanServiceDBCalls(t *testing.T) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	err := filepath.Walk(serviceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		// 注意：不能用「文件是否 import gorm」做短路 —— 链式调用靠类型推断，
		// 不 import gorm 也能直查（如 datarule_crud.go 的 query := s.drm.DB(ctx)）。
		rel, _ := filepath.Rel(serviceRoot, path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// 函数级 db 会话变量：*gorm.DB 参数 + 赋值自 .DB(...) 调用链的标识符。
			dbVars := map[string]bool{}
			ast.Inspect(fd, func(n ast.Node) bool {
				if ft, ok := n.(*ast.FuncType); ok {
					for _, field := range ft.Params.List {
						if isGormDBType(field.Type) {
							for _, name := range field.Names {
								dbVars[name.Name] = true
							}
						}
					}
				}
				if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) > 0 && chainsToDBCall(as.Rhs[0]) {
					for _, name := range as.Lhs {
						if id, ok := name.(*ast.Ident); ok {
							dbVars[id.Name] = true
						}
					}
				}
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := ce.Fun.(*ast.SelectorExpr)
				if !ok || !gormOps[sel.Sel.Name] {
					return true
				}
				if dbReceiver(sel.X, dbVars) {
					line := fset.Position(ce.Pos()).Line
					out = append(out, fmt.Sprintf("%s:%d %s.%s",
						rel, line, fd.Name.Name, sel.Sel.Name))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 service 目录失败: %v", err)
	}
	sort.Strings(out)
	return out
}

// isGormDBType 判断类型表达式是否 *gorm.DB。
func isGormDBType(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "DB"
}

// chainsToDBCall 判断表达式链中是否含 .DB( 的 selector 调用（如 s.am.DB(ctx).Where(...)）。
// 必须递归深入 SelectorExpr.X 与 CallExpr.Fun，否则链式调用的中间环节会遮住链首的 .DB(。
func chainsToDBCall(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "DB" {
			return true
		}
		return chainsToDBCall(e.Fun)
	case *ast.SelectorExpr:
		return chainsToDBCall(e.X)
	case *ast.ParenExpr:
		return chainsToDBCall(e.X)
	}
	return false
}

// dbReceiver 判断 receiver 是否落在 DB 会话上：链中含 .DB( 调用，或根标识符是会话变量。
func dbReceiver(expr ast.Expr, dbVars map[string]bool) bool {
	for {
		switch e := expr.(type) {
		case *ast.CallExpr:
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "DB" {
				return true
			}
			expr = e.Fun
		case *ast.SelectorExpr:
			if id, ok := e.X.(*ast.Ident); ok {
				return dbVars[id.Name]
			}
			expr = e.X
		case *ast.Ident:
			return dbVars[e.Name]
		default:
			return false
		}
	}
}

// knownDeviations 已知偏差表（CQ-025，只增不减）。生成方式：scanServiceDBCalls 实测输出。
// 每行格式：文件:行号 函数名.gorm方法。
//
// 2026-09 数据权限快照整改的账实变更：
//   - datarule_provider.go 的 4 处直查（GetRules 每次查库）随快照改造消失，从表中移除；
//   - 新增 datarule_snapshot.go 的 6 处 —— 那是**快照重建**路径（3 条加载查询），
//     由 lazy 首次命中 / 写路径同步重载 / 5 分钟兜底刷新触发，不在查询热路径上；
//   - datarule_crud.go 的行号随 RuleCreate/RuleUpdate/RuleDelete 里新增的重载调用下移。
var knownDeviations = []string{
	"admin.go:138 AdminCreate.Create",
	"admin.go:153 AdminDetail.Scan",
	"admin.go:153 AdminDetail.Where",
	"admin.go:221 AdminEdit.Updates",
	"admin.go:221 AdminEdit.Where",
	"admin.go:34 AdminList.Where",
	"admin.go:336 AdminListByDeptID.Where",
	"admin.go:339 AdminListByDeptID.Count",
	"admin.go:345 AdminListByDeptID.Find",
	"admin.go:37 AdminList.Where",
	"admin.go:378 AdminBatchSetDeptID.Model",
	"admin.go:378 AdminBatchSetDeptID.Update",
	"admin.go:378 AdminBatchSetDeptID.Where",
	"admin.go:386 AdminCountByDeptID.Count",
	"admin.go:386 AdminCountByDeptID.Where",
	"admin.go:40 AdminList.Where",
	"admin.go:43 AdminList.Where",
	"admin.go:46 AdminList.Count",
	"admin.go:525 AdminLogin.First",
	"admin.go:525 AdminLogin.Where",
	"admin.go:566 AdminLogin.Updates",
	"admin.go:566 AdminLogin.Where",
	"admin.go:67 AdminList.Find",
	"admin.go:720 DevLogin.Where",
	"admin.go:722 DevLogin.Where",
	"admin.go:724 DevLogin.First",
	"datarule.go:315 RuleList.Where",
	"datarule.go:318 RuleList.Where",
	"datarule.go:323 RuleList.Count",
	"datarule.go:330 RuleList.Scan",
	"datarule.go:864 loadDataRuleSnapshotFromDB.Joins",
	"datarule.go:864 loadDataRuleSnapshotFromDB.Scan",
	"datarule.go:864 loadDataRuleSnapshotFromDB.Where",
	"datarule.go:902 loadDataRuleSnapshotFromDB.Scan",
	"datarule.go:902 loadDataRuleSnapshotFromDB.Where",
	"datarule.go:917 loadDataRuleSnapshotFromDB.Scan",
	"role.go:555 PermList.Where",
	"role.go:558 PermList.Where",
	"role.go:561 PermList.Where",
	"role.go:564 PermList.Where",
	"role.go:568 PermList.Count",
	"role.go:574 PermList.Scan",
	"role.go:593 PermDetail.Scan",
	"role.go:593 PermDetail.Where",
	"role.go:613 PermOptions.Where",
	"role.go:615 PermOptions.Where",
	"role.go:617 PermOptions.Find",
}

// TestAdminServiceDirectDBDeviations 守卫：service 层直查 DB 只允许出现在已知偏差表内。
func TestAdminServiceDirectDBDeviations(t *testing.T) {
	actual := scanServiceDBCalls(t)
	known := map[string]bool{}
	for _, k := range knownDeviations {
		known[k] = true
	}
	var unknown, resolved []string
	for _, a := range actual {
		if !known[a] {
			unknown = append(unknown, a)
		}
	}
	inTable := map[string]bool{}
	for _, a := range actual {
		inTable[a] = true
	}
	for _, k := range knownDeviations {
		if !inTable[k] {
			resolved = append(resolved, k)
		}
	}
	for _, u := range unknown {
		t.Errorf("新增未登记的 service 层直查 DB（请登记进 knownDeviations）: %s", u)
	}
	for _, r := range resolved {
		t.Errorf("已知偏差已不存在（请从 knownDeviations 移除，保持账实一致）: %s", r)
	}
	if testing.Verbose() {
		t.Logf("当前实测 %d 处直查", len(actual))
	}
}

// TestPrintAdminServiceDirectDB 偏差表生成辅助：go test -v -run TestPrintAdminServiceDirectDB 输出实测清单。
func TestPrintAdminServiceDirectDB(t *testing.T) {
	for _, a := range scanServiceDBCalls(t) {
		t.Logf("%q +", a)
	}
}
