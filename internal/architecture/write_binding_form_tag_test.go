package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var pendingFormTagDTOs = map[string]int{}

var intentionallyJSONOnlyDTOs = map[string]bool{
	"internal/module/ai/inbound/http:sessionAppendBody":           true,
	"internal/module/ai/inbound/http:sessionFoldBody":             true,
	"internal/module/ai/inbound/http:sessionFoldPlanBody":         true,
	"internal/module/ai/inbound/http:sessionRenameBody":           true,
	"internal/module/ai/inbound/http:tokenCreateBody":             true,
	"internal/module/ai/inbound/http:tokenRevokeBody":             true,
	"internal/module/build/inbound/http:retryReq":                 true,
	"internal/module/product/inbound/http:productPricingApplyReq": true,
}

func TestWriteDTOsHaveFormTag(t *testing.T) {
	root := repoRoot(t)
	roots := []string{
		filepath.Join(root, "internal", "module"),
		// 后台页面渲染入口已并入 pkg/view：它现在持有页面/片段出口，
		// 与模块同属「写端点绑定 DTO」的扫描面。
		filepath.Join(root, "pkg", "view"),
	}

	used := map[string]bool{}

	fset := token.NewFileSet()
	for _, dir := range roots {
		files := goFilesUnder(t, dir, func(string) bool { return true })
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				// 表单协议的唯一来源已是显式表单入口：
				//   binding.ParseForm[T](c)      —— 泛型显式表单
				//   c.ShouldBindWith(&req, FormPost) —— 原生表单
				// 兼容入口 binding.JSON / binding.Form 已从写路由清退（只剩 GET 读端点）。
				// 只看兼容入口会让本判据在迁移完成后空转，因此按真实表单入口收面。
				call, okCall := n.(*ast.CallExpr)
				if okCall {
					if name := formEntryTypeOf(call); name != "" {
						used[name] = true
					}
					return true
				}
				return true
			})
		}
	}

	hits := map[string]int{}
	for _, dir := range roots {
		files := goFilesUnder(t, dir, func(string) bool { return true })
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel := relDir(root, path)
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				if !used[ts.Name.Name] {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				if !structLacksFormTag(st, implementsBindForm(fset, used, ts.Name.Name, roots, root)) {
					return true
				}
				key := rel + ":" + ts.Name.Name
				if intentionallyJSONOnlyDTOs[key] {
					return true
				}
				hits[key] = 1
				return true
			})
		}
	}

	assertBudget(t, hits, pendingFormTagDTOs, "写 DTO 的字段都带 form tag（htmx 表单靠它绑定）")
}

func structLacksFormTag(st *ast.StructType, hasBindForm bool) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		if !field.Names[0].IsExported() {
			continue
		}
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		jsonTag := tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		if tag.Get("form") != "" {
			continue
		}

		if isRawMessageField(field.Type) && hasBindForm {
			continue
		}
		return true
	}
	return false
}

func isRawMessageField(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "RawMessage"
}

func typeNameOf(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return typeNameOf(e.X)
	}
	return ""
}

func relDir(root, path string) string {
	dir := filepath.Dir(path)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(rel)
}

func implementsBindForm(_ *token.FileSet, _ map[string]bool, typeName string, roots []string, root string) bool {
	dirs := make([]string, 0, len(roots))
	dirs = append(dirs, roots...)
	for _, dir := range dirs {
		files := make([]string, 0, 64)
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		for _, path := range files {
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Name.Name != "BindForm" {
					continue
				}
				if len(fn.Recv.List) == 0 {
					continue
				}
				if recvTypeName(fn.Recv.List[0].Type) == typeName {
					return true
				}
			}
		}
	}
	return false
}

func recvTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return recvTypeName(e.X)
	}
	return ""
}

// formEntryTypeOf 识别显式表单绑定的请求类型名。
//
//	binding.ParseForm[T](c)                     → "T"
//	c.ShouldBindWith(&req, ginbinding.FormPost)  → req 的声明类型名
//
// 只认这两种：兼容入口 binding.JSON/Form 已从写路由清退，不该再作为判据来源。
func formEntryTypeOf(call *ast.CallExpr) string {
	if call == nil {
		return ""
	}
	// binding.ParseForm[T](c)：泛型调用的被调方是 IndexExpr。
	if idx, ok := call.Fun.(*ast.IndexExpr); ok {
		if sel, okSel := idx.X.(*ast.SelectorExpr); okSel && sel.Sel.Name == "ParseForm" {
			return typeNameOf(idx.Index)
		}
	}
	// c.ShouldBindWith(&req, FormPost|FormMultipart)：第二参数是表单 binder。
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "ShouldBindWith" || len(call.Args) != 2 {
		return ""
	}
	binder, okBinder := call.Args[1].(*ast.SelectorExpr)
	if !okBinder || (binder.Sel.Name != "FormPost" && binder.Sel.Name != "FormMultipart") {
		return ""
	}
	return typeNameOf(call.Args[0])
}
