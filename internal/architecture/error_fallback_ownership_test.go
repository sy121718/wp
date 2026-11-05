package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

var fallbackExemptModules = map[string]string{}

func TestErrorFallbacksCoverDeclarations(t *testing.T) {
	root := repoRoot(t)
	enumsDirs := map[string]string{}
	for _, f := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"enums"+string(filepath.Separator)) &&
			!strings.HasSuffix(p, "_test.go")
	}) {
		enumsDirs[filepath.Dir(f)] = f
	}

	checkedPkgs, checkedConsts := 0, 0
	for dir := range enumsDirs {
		mod := filepath.Base(filepath.Dir(dir))
		if _, ok := fallbackExemptModules[mod]; ok {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		declared := map[string]string{}
		fallbacks := map[string]bool{}
		hasErrFactory := false

		pkgConsts := map[string]string{}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			for k, v := range constStringValues(t, file) {
				pkgConsts[k] = v
			}
		}

		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatalf("解析 %s 失败: %v", file, err)
			}
			for _, decl := range parsed.Decls {

				if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
					for _, spec := range gen.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for i, name := range vs.Names {
							if !strings.HasPrefix(name.Name, "Err") || i >= len(vs.Values) {
								continue
							}
							lit, ok := vs.Values[i].(*ast.BasicLit)
							if !ok || lit.Kind != token.STRING {
								continue
							}
							v, uerr := strconv.Unquote(lit.Value)
							if uerr != nil {
								t.Fatalf("%s %s: %v", file, name.Name, uerr)
							}
							declared[v] = name.Name
						}
					}
				}

				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "Err" {
					hasErrFactory = true
				}
			}

			for _, name := range errorFallbackTableNames(t, file) {
				for _, entry := range mapLiteralKeys(t, file, name, pkgConsts) {
					fallbacks[entry] = true
				}
			}
		}
		if len(declared) == 0 {
			continue
		}
		checkedPkgs++
		checkedConsts += len(declared)

		if !hasErrFactory {
			t.Errorf("%s：有 Err* 常量但没有 Err(key) 工厂 —— service 只能用 errors.New 造裸 key", dir)
		}
		for value, name := range declared {
			if !facing.IsBusinessKey(value) {
				t.Errorf("%s：%s = %q 过不了 pkg/facing.IsBusinessKey —— 出口会把它当成内部错误（文案归词库，key 形态必须成立）",
					relToRoot(t, root, dir), name, value)
			}
		}
		for value := range fallbacks {
			if _, ok := declared[value]; !ok {
				t.Errorf("%s：错误兜底表里的 %q 不对应任何 Err* 常量（拼写错误或常量已删）",
					relToRoot(t, root, dir), value)
			}
		}
	}
	if checkedPkgs == 0 || checkedConsts < 100 {
		t.Fatalf("只扫到 %d 个包 / %d 条常量：判据失效（源码结构变了？）", checkedPkgs, checkedConsts)
	}
}

func TestNoBareKeyBusinessErrors(t *testing.T) {
	root := repoRoot(t)
	offenders := []string{}
	for _, file := range goFilesUnder(t, filepath.Join(root, "internal"), func(p string) bool {
		return !strings.HasSuffix(p, "_test.go")
	}) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "errors" {
				return true
			}
			if len(call.Args) != 1 {
				return true
			}

			arg, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkgIdent, ok := arg.X.(*ast.Ident); !ok || !strings.HasSuffix(pkgIdent.Name, "enums") {
				return true
			}
			pos := fset.Position(call.Pos())
			offenders = append(offenders, relToRoot(t, root, file)+":"+strconv.Itoa(pos.Line)+
				"：errors.New("+pkgName(arg)+") —— 用 enums.Err("+pkgName(arg)+") 带上兜底")
			return true
		})
	}
	for _, o := range offenders {
		t.Error(o)
	}
}

func pkgName(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

func errorFallbackTableNames(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil
	}
	var out []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for _, id := range vs.Names {
			if id.Name == "errFallbacks" || isExportedErrFallbacks(id.Name) {
				out = append(out, id.Name)
			}
		}
		return true
	})
	return out
}

func isExportedErrFallbacks(name string) bool {
	if name == "ErrDetailFallbacks" || !strings.HasSuffix(name, "ErrFallbacks") {
		return false
	}
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func constStringValues(t *testing.T, file string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return out
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, e := strconv.Unquote(lit.Value); e == nil {
						out[n.Name] = v
					}
				}
			}
		}
	}
	return out
}

func mapLiteralKeys(t *testing.T, file, name string, consts map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil
	}
	var out []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		named := false
		for _, id := range vs.Names {
			if id.Name == name {
				named = true
			}
		}
		if !named {
			return true
		}
		for _, val := range vs.Values {
			lit, ok := val.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch k := kv.Key.(type) {
				case *ast.BasicLit:
					if v, e := strconv.Unquote(k.Value); e == nil {
						out = append(out, v)
					}
				case *ast.Ident:
					if v, ok := consts[k.Name]; ok {
						out = append(out, v)
					}
				}
			}
		}
		return true
	})
	return out
}
