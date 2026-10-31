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

const serviceRoot = "../service"

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

		rel, _ := filepath.Rel(serviceRoot, path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}

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

func isGormDBType(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "DB"
}

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

func TestPrintAdminServiceDirectDB(t *testing.T) {
	for _, a := range scanServiceDBCalls(t) {
		t.Logf("%q +", a)
	}
}
