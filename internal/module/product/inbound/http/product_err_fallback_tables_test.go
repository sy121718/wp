package producthttp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

func enumsStringConstants(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "enums")
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

func TestSkipReasonFallbacksCoverEnumsSkipKeys(t *testing.T) {
	all := enumsStringConstants(t)
	tables := []struct {
		name   string
		prefix string
		table  map[string]string
	}{
		{"VariantSkipFallbacks", "VariantSkip", productenums.VariantSkipFallbacks},
	}
	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			for val, name := range all {
				if !strings.HasPrefix(name, tc.prefix) {
					continue
				}
				text := strings.TrimSpace(tc.table[val])
				if text == "" {
					t.Errorf("%s (=%q) 没有中文兜底，页面上会显示裸 key", name, val)
					continue
				}
				if text == val {
					t.Errorf("%s (=%q) 的兜底就是 key 本身 —— 等于没有兜底", name, val)
				}
			}

			for key := range tc.table {
				if _, ok := all[key]; !ok {
					t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
				}
			}
		})
	}
}

func TestBusinessErrorFactCarriesOnlyKey(t *testing.T) {
	c, _ := newHXContext(t, "", "")
	key := productenums.ErrNameRequired
	fact, ok := facing.FactOf(productenums.Err(key))
	if !ok || !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v ok=%v", fact, ok)
	}
	if fact.Key != key {
		t.Errorf("事实 key = %q，want %q", fact.Key, key)
	}
	if fact.Fallback != "" {
		t.Errorf("错误声明不该再带中文兜底（%q）：文案归词库，缺词由 JSON 出口给共享提示", fact.Fallback)
	}
	if got := response.MessageOf(c, "product", productenums.Err(key)); got != response.InternalText(c) {
		t.Errorf("产品错误 key 没在测试词库里，出口应给共享归口文案，实际 %q", got)
	}
}

func TestBusinessErrDetailStaysInTail(t *testing.T) {
	err := fmt.Errorf("%w: 第 3 行", productenums.Err(productenums.ErrNameRequired))
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v", fact)
	}
	if fact.Key != productenums.ErrNameRequired {
		t.Fatalf("key = %q，want %q", fact.Key, productenums.ErrNameRequired)
	}
	if fact.Tail != "第 3 行" {
		t.Fatalf("明细 = %q，want %q", fact.Tail, "第 3 行")
	}
}
