package productservice

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// product_bundle_skip_test.go — 成员来源解析的「跳过原因」必须句句有中文原文。
//
// 跳过原因不是业务错误（取值过不了 pkg/facing 的业务判据），所以 enums 那层不给它们配兜底；
// bundleMemberSkipFallbacks 就是唯一真源，漏一条页面上显示的就是裸 key
// （BundleMemberVariantDisabled 这种串，运营只会以为后台坏了）。

// TestBundleMemberSkipFallbacksCoverEnumsKeys 每条 BundleMember* 常量都要在表里有原文。
func TestBundleMemberSkipFallbacksCoverEnumsKeys(t *testing.T) {
	all := enumsStringConstantsOf(t)
	covered := 0
	for val, name := range all {
		if !strings.HasPrefix(name, "BundleMember") {
			continue
		}
		covered++
		text := strings.TrimSpace(bundleMemberSkipFallbacks[val])
		if text == "" {
			t.Errorf("%s (=%q) 没有中文兜底，页面上会显示裸 key", name, val)
			continue
		}
		if text == val {
			t.Errorf("%s (=%q) 的兜底就是 key 本身 —— 等于没有兜底", name, val)
		}
	}
	if covered == 0 {
		t.Fatal("没扫到任何 BundleMember* 常量 —— 守卫本身失效了")
	}
	// 反向：表里的 key 必须仍是 enums 里声明的常量（改名后没同步 → 这张表退化成死条目）。
	for key := range bundleMemberSkipFallbacks {
		if _, ok := all[key]; !ok {
			t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
		}
	}
}

// enumsStringConstantsOf 扫 product enums 包里全部字符串常量：取值 → 常量名。
//
// 从**源文件**取而不是在测试里抄清单：抄清单的话，新增一个跳过原因只要不同步改测试，
// 守卫就一直绿 —— 那正是要拦的事。
func enumsStringConstantsOf(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "enums")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", dir, err)
	}
	fset := token.NewFileSet()
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", name, perr)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("常量 %s 的取值不是合法字符串: %v", id.Name, uerr)
					}
					out[val] = id.Name
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("没扫到任何字符串常量 —— 守卫本身失效了")
	}
	return out
}
