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

// admin 的 service 层不再持有 `model.DB(ctx)` 句柄做 CRUD/list/count/scan/joins：
// 数据库筛选、聚合与分页落在所属 model，service 只做业务校验与事务编排。
// 这里不给偏差台账 —— 一旦出现直查就是零容忍的失败，靠改 model 解决，不是登记进来。
func TestAdminServiceHasNoDirectDBAccess(t *testing.T) {
	actual := scanServiceDBCalls(t)
	for _, a := range actual {
		t.Errorf("service 层直接拿 DB 句柄查库（请把这条查询下沉到 admin/model）: %s", a)
	}
	if len(actual) != 0 {
		t.Logf("提示：先跑 -run TestPrintAdminServiceDirectDB 看完整清单再逐条迁移")
	}
}

func TestPrintAdminServiceDirectDB(t *testing.T) {
	for _, a := range scanServiceDBCalls(t) {
		t.Logf("%q +", a)
	}
}
