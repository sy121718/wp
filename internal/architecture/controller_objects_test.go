package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// isObjectQueryPipeline 验证绑定、原对象查询、命名出口的语法形状。
// 不认模块名和方法名单；循环、格式化、算术、map 与业务写调用都会退出此路径。
func isObjectQueryPipeline(body *ast.BlockStmt) bool {
	objects, binding, valid := false, false, true
	bound := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if !valid {
			return false
		}
		switch x := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.GoStmt, *ast.DeferStmt, *ast.SendStmt, *ast.IncDecStmt, *ast.FuncLit, *ast.MapType, *ast.IndexExpr:
			// binding.Query[T] 的 IndexExpr 是类型实例化，另在调用节点验证。
			if _, ok := n.(*ast.IndexExpr); !ok {
				valid = false
			}
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ && x.Op != token.LAND && x.Op != token.LOR {
				valid = false
			}
		case *ast.UnaryExpr:
			if x.Op != token.AND && x.Op != token.NOT {
				valid = false
			}
		case *ast.ValueSpec:
			if len(x.Values) != 0 {
				valid = false
			}
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE && x.Tok != token.ASSIGN {
				valid = false
			}
			// 只允许修改绑定请求的筛选范围，不能在出口前重写查询对象。
			for _, lhs := range x.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					root, ok := sel.X.(*ast.Ident)
					if !ok || !bound[root.Name] {
						valid = false
					}
				}
			}
			if len(x.Rhs) == 1 {
				if call, ok := x.Rhs[0].(*ast.CallExpr); ok {
					if generic, ok := call.Fun.(*ast.IndexExpr); ok {
						if sel, ok := generic.X.(*ast.SelectorExpr); ok {
							if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "binding" && sel.Sel.Name == "Query" {
								if id, ok := x.Lhs[0].(*ast.Ident); ok {
									bound[id.Name] = true
								}
							}
						}
					}
				}
			}
			for _, rhs := range x.Rhs {
				switch rhs.(type) {
				case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
				default:
					valid = false
				}
			}
		case *ast.CallExpr:
			fun := x.Fun
			if generic, ok := fun.(*ast.IndexExpr); ok {
				fun = generic.X
			}
			sel, ok := fun.(*ast.SelectorExpr)
			if !ok {
				valid = false
				break
			}
			if pkg, ok := sel.X.(*ast.Ident); ok {
				switch pkg.Name + "." + sel.Sel.Name {
				case "binding.Query":
					binding = true
				case "pageview.Objects":
					objects = true
				case "pageview.Object":
					if len(x.Args) != 2 {
						valid = false
						break
					}
					if _, ok := x.Args[0].(*ast.BasicLit); !ok {
						valid = false
					}
					switch x.Args[1].(type) {
					case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.BinaryExpr:
					default:
						valid = false
					}
				case "pageview.Render", "pageview.Fail":
				default:
					valid = false
				}
				break
			}
			if sel.Sel.Name == "Context" {
				break
			}
			receiver, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				valid = false
				break
			}
			root, ok := receiver.X.(*ast.Ident)
			if !ok || root.Name != "h" {
				valid = false
				break
			}
			read := false
			for _, prefix := range []string{"Query", "Get", "List", "Resolve", "Catalog", "Search", "CategoryNodes", "ProductDetailPath"} {
				if strings.HasPrefix(sel.Sel.Name, prefix) {
					read = true
				}
			}
			if !read {
				valid = false
			}
		}
		return true
	})
	return valid && objects && binding
}

func TestObjectQueryPipelineRejectsDerivedShapes(t *testing.T) {
	base := `package sample; func page(c *gin.Context) { req, ok := binding.Query[dto.Req](c); if !ok { return }; scope, err := h.svc.ResolveScope(c.Request.Context(), req.ProjectID); if err != nil { pageview.Fail(c, err); return }; result, err := h.svc.Query(c.Request.Context(), req); if err != nil { pageview.Fail(c, err); return }; PLACEHOLDER; pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Result", result))) }`
	for _, tc := range []struct {
		code string
		want bool
	}{
		{"req.ProjectID = scope.ProjectID", true},
		{"result.Total = result.Total + 1", false},
		{"result.Label = req.Keyword", false},
		{"result.Label = scope.ProjectID", false},
		{"var display = dto.Display{Label: req.Keyword}", false},
		{"pageview.Object(\"Display\", &dto.Display{Label: req.Keyword})", false},
		{"pageview.Object(\"Label\", \"商品\")", false},
		{"c.Redirect(http.StatusFound, \"/admin/products\")", false},
		{"for _, row := range result.Items { row.Label = fmt.Sprint(row.ID) }", false},
		{"result = h.svc.Update(c.Request.Context(), req)", false},
		{"result = map[string]any{\"items\": result}", false},
	} {
		src := strings.Replace(base, "PLACEHOLDER", tc.code, 1)
		f, err := parser.ParseFile(token.NewFileSet(), "pipeline.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		got := isObjectQueryPipeline(f.Decls[0].(*ast.FuncDecl).Body)
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.code, got, tc.want)
		}
	}
}
