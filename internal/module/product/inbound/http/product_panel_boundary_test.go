package producthttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Page handlers hand off data without reading or assembling the template panel.
func TestProductPageHandlerDoesNotAssembleTemplatePanel(t *testing.T) {
	for _, name := range []string{"product_subject_handle.go", "product_detail_template_handle.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "GetByEntity", "ResolveTemplate", "ListSnapshots", "CountByTemplate", "detailTemplatePanel":
						t.Errorf("%s: 控制器直接查询或装配模板面板: %s", fset.Position(node.Pos()), sel.Sel.Name)
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "TplPanel" {
						t.Errorf("%s: 控制器补写 TplPanel", fset.Position(sel.Pos()))
					}
				}
			}
			return true
		})
	}
}
