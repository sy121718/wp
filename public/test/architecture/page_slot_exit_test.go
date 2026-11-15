package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func pageSlotWiredHandlers(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, file := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"inbound"+string(filepath.Separator)+"http"+string(filepath.Separator)) &&
			!strings.HasSuffix(p, "_test.go")
	}) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "Any":
			default:
				return true
			}
			hasSlot := false
			handler := ""
			for _, arg := range call.Args {
				inner, ok := arg.(*ast.CallExpr)
				if !ok {

					if s, ok := arg.(*ast.SelectorExpr); ok {
						handler = s.Sel.Name
					}
					continue
				}
				// 页面渲染槽的现行名称是 pageview.Slot；PageSlot 是已清退的旧名，
				// 两者都认，避免改名后判据静默扫不到任何路由。
				if fs, ok := inner.Fun.(*ast.SelectorExpr); ok && (fs.Sel.Name == "Slot" || fs.Sel.Name == "PageSlot") {
					hasSlot = true
				}
			}
			if hasSlot && handler != "" {
				out[handler] = fset.Position(call.Pos()).String()
			}
			return true
		})
	}
	return out
}

func controllerCallsCHTML(t *testing.T, root, method string) []string {
	t.Helper()
	var hits []string
	for _, file := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"inbound"+string(filepath.Separator)+"http"+string(filepath.Separator)) &&
			!strings.HasSuffix(p, "_test.go")
	}) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Name.Name != method || fd.Body == nil {
				return true
			}
			ast.Inspect(fd.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				s, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || s.Sel.Name != "HTML" {
					return true
				}
				hits = append(hits, fset.Position(call.Pos()).String())
				return true
			})
			return true
		})
	}
	return hits
}

func TestPageSlotHandlersUseRenderOnly(t *testing.T) {
	root := repoRoot(t)
	wired := pageSlotWiredHandlers(t, root)

	if len(wired) < 27 {
		t.Fatalf("只找到 %d 个挂 PageSlot 的 handler：判据失效（路由写法变了？）", len(wired))
	}
	for method, pos := range wired {
		for _, hit := range controllerCallsCHTML(t, root, method) {
			t.Errorf("%s 挂在 PageSlot 上（%s），但方法体里还有 c.HTML（%s）\n"+
				"  半迁移：模板名归了路由层，控制器却自己渲染 —— PageSlot 挂的那个模板永远不会被用到",
				method, relToRoot(t, root, pos), relToRoot(t, root, hit))
		}
	}
}
