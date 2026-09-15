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
var knownDeviations = []string{
	"admin_crud.go:133 AdminCreate.Create",
	"admin_crud.go:148 AdminDetail.Scan",
	"admin_crud.go:148 AdminDetail.Where",
	"admin_crud.go:216 AdminEdit.Updates",
	"admin_crud.go:216 AdminEdit.Where",
	"admin_crud.go:284 AdminListByDeptID.Where",
	"admin_crud.go:287 AdminListByDeptID.Count",
	"admin_crud.go:29 AdminList.Where",
	"admin_crud.go:293 AdminListByDeptID.Find",
	"admin_crud.go:32 AdminList.Where",
	"admin_crud.go:326 AdminBatchSetDeptID.Model",
	"admin_crud.go:326 AdminBatchSetDeptID.Update",
	"admin_crud.go:326 AdminBatchSetDeptID.Where",
	"admin_crud.go:334 AdminCountByDeptID.Count",
	"admin_crud.go:334 AdminCountByDeptID.Where",
	"admin_crud.go:35 AdminList.Where",
	"admin_crud.go:38 AdminList.Where",
	"admin_crud.go:41 AdminList.Count",
	"admin_crud.go:62 AdminList.Find",
	"admin_login.go:241 DevLogin.Where",
	"admin_login.go:243 DevLogin.Where",
	"admin_login.go:245 DevLogin.First",
	"admin_login.go:46 AdminLogin.First",
	"admin_login.go:46 AdminLogin.Where",
	"admin_login.go:87 AdminLogin.Updates",
	"admin_login.go:87 AdminLogin.Where",
	"datarule_crud.go:165 RuleList.Where",
	"datarule_crud.go:168 RuleList.Where",
	"datarule_crud.go:173 RuleList.Count",
	"datarule_crud.go:180 RuleList.Scan",
	"datarule_provider.go:30 GetRules.Find",
	"datarule_provider.go:30 GetRules.Where",
	"datarule_provider.go:85 GetRules.Find",
	"datarule_provider.go:85 GetRules.Where",
	"datarule_provider.go:85 GetRules.Where",
	"perm_crud.go:22 PermList.Where",
	"perm_crud.go:25 PermList.Where",
	"perm_crud.go:28 PermList.Where",
	"perm_crud.go:31 PermList.Where",
	"perm_crud.go:35 PermList.Count",
	"perm_crud.go:41 PermList.Scan",
	"perm_crud.go:60 PermDetail.Scan",
	"perm_crud.go:60 PermDetail.Where",
	"perm_crud.go:80 PermOptions.Where",
	"perm_crud.go:82 PermOptions.Where",
	"perm_crud.go:84 PermOptions.Find",
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
