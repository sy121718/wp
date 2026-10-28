package architecture

// error_fallback_ownership_test.go — 错误兜底的归属（全仓一条判据）。
//
// ## 判据
//
// **业务错误的对外文案由声明处给，不由出口猜。**
//
// 落到两个可判定的形状：
//
//  1. 每个 enums 包里的 `Err*` 常量，值必须在同包的**错误兜底表**里有一句中文兜底；
//     反过来，表里的每个 key 必须对应一条真实常量（防错别字、防死条目）。
//
//     兜底表的名字不钉死：`errFallbacks`（多数模块）与首字母大写的 `*ErrFallbacks`
//     （product 的 `ProductErrFallbacks`）都算。钉名字的代价是**改了名字的门禁会盯着一张
//     没人读的表**——product 曾同时存在 `errFallbacks`（死表）与 `ProductErrFallbacks`（活表），
//     门禁验的是死表，于是活表缺条目时线上文案是对的、门禁却红，反之亦然。
//     明细词条表（`ErrDetailFallbacks`）与小写前缀表（webhook 的 `deliveryErrFallbacks`）
//     的 key 不是 `Err*` 常量，不参与本条判据。
//  2. service / model 层不许用 `errors.New(xxxenums.ErrYyy)` 构造业务错误 ——
//     那样构造出来的是**裸 key**：`facing.FactOf` 只能把 key 本身当兜底，
//     词条库读不到时客户端拿到的就是 `order.err.orderNotFound` 这样的字符串。
//
// ## 为什么这条值得单独一道门禁
//
// 改造前全仓有 678 处 `errors.New(enums.ErrXxx)`。它们的症状是**静默的**：
// 词条在场时一切正常（出口按 key 查到译文），只有词条缺席（迁移没跑 / 库不可用）
// 才露出裸 key —— 而那种环境恰恰是最需要看清提示的时候。
//
// 更麻烦的是它不可数：`facing.FactOf` 对裸 key 返回 `Fact{Fallback: key}`，
// 看起来"有兜底"，实际上兜底就是 key。所以这条判据必须查**声明处**，不能查出口。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fallbackExemptModules 允许不写 errFallbacks 的 enums 包 → 理由（必须写清，这张表要被审计）。
var fallbackExemptModules = map[string]string{
	// 空表：目前每个有 Err* 常量的 enums 包都必须给兜底。
	// 某模块确实不需要（例如它的 Err* 全是内部哨兵、从不对外）时在这里登记并说明理由。
}

// TestErrorFallbacksCoverDeclarations 每个 Err* 常量都有中文兜底，且兜底表没有死条目。
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
		declared := map[string]string{} // 常量值 → 常量名
		fallbacks := map[string]bool{}  // 错误兜底表里出现的 key（字面量值）
		hasErrFactory := false

		// 先把整包的 const 表建起来：兜底表的 key 是常量标识符，
		// 而常量与表可能不在同一个文件里（ai 的常量分散在 7 个文件、表集中在一处）。
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
				// ① 扫 Err* 常量
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
				// ② 扫函数声明里的 errFallbacks 表与 Err 工厂
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "Err" {
					hasErrFactory = true
				}
			}
			// 兜底表是 var 块里的复合字面量，**在包内任意一个文件里**都可能
			// （ai 的常量分散在 7 个文件、兜底表集中在一处）。这里对包里每个文件都扫一遍，
			// 把该文件里所有「错误兜底表」的 key 并进来（见 isErrorFallbackTable）。
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
			if !fallbacks[value] {
				t.Errorf("%s：%s = %q 没有在错误兜底表里给中文兜底", relToRoot(t, root, dir), name, value)
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

// TestNoBareKeyBusinessErrors service 层不许用 errors.New(enums.ErrXxx) 造业务错误。
//
// 那条路绕过声明处的兜底：`facing.FactOf` 会把 key 本身当兜底，
// 词条缺席时客户端直接看到 `order.err.orderNotFound`。
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
			// 参数形如 xxxenums.ErrYyy → 裸 key
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

// errorFallbackTableNames 一个文件里所有「错误兜底表」的变量名。
//
// 判据只认两种确切形态，不用「以 Fallbacks 结尾」这类模糊前缀 —— 那会把明细词条表
// （ErrDetailFallbacks，key 是 admin.* 界面词条）、跳过原因表（VariantSkipFallbacks）、
// 投递编码串判定表（deliveryErrFallbacks，key 是 DeliveryErr* 常量）一并扫进来，
// 它们的 key 不对应 Err* 常量，反向判据会立刻全红。
//
//	① errFallbacks                 —— 多数模块的形态
//	② 导出名 *ErrFallbacks         —— product 的 ProductErrFallbacks（唯一例外）
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

// isExportedErrFallbacks 名字形如 ProductErrFallbacks / 任何导出名 + ErrFallbacks。
//
// 排除 ErrDetailFallbacks：它是明细词条的兜底表（key 形如 admin.products.detail.*）。
func isExportedErrFallbacks(name string) bool {
	if name == "ErrDetailFallbacks" || !strings.HasSuffix(name, "ErrFallbacks") {
		return false
	}
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// mapLiteralKeys 取 `var <name> = map[string]string{ K: V, ... }` 里 K 的字符串值。
//
// K 可能是常量标识符（`ErrInvalidParam: "..."`）：那就回查同包的 const 声明。
// constStringValues 取一个文件里 `NAME = "literal"` 形式的常量（标识符 → 值）。
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
