package producthttp

// product_err_fallback_tables_test.go — 出口层兜底表的守卫 + 「未登记业务 key」的稳定兜底（task-158 §7.3）。
//
// 三张表各有归属，守卫也各在各处：
//   · 本模块 Err* 的展示原文 → internal/module/product/enums 的 ProductErrFallbacks
//     （覆盖面守卫在 productenums 包里）；
//   · 别模块 key（inventory / presentation）→ 各自的 enums 兜底表（product 不再替对方留补丁表）；
//   · 跳过原因（VariantSkip* / BundleMember*）→ productenums.BundleMemberSkipFallbacks ——
//     它们的取值过不了业务判据（pkg/facing.IsBusinessKey），所以单独一张表（漏登一条 =
//     页面上出现裸 key）；覆盖面守卫在 enums 包，本文件守「出口取词时用的是这张表」。

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

// enumsStringConstants 扫 product enums 包里全部字符串常量：取值 → 常量名。
//
// 直接从**源文件**取（而不是在测试里抄一份清单）：抄清单的话，新增一个跳过原因只要不同步改测试，
// 守卫就一直绿 —— 那正是要拦的事。
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

// TestSkipReasonFallbacksCoverEnumsSkipKeys 每条跳过原因都要有出口层的展示原文。
//
// 跳过原因不走 productErrFactOf（取值过不了业务判据），所以它们的表与 ProductErrFallbacks 分开；
// 两张表现在都在 enums（声明处）：VariantSkip* 与 BundleMember* 同住
// productenums.BundleMemberSkipFallbacks，覆盖面守卫在 enums 包的
// product_bundle_skip_fallbacks_test.go —— 这里只留「出口取词时用的是不是这张表」这一条。
func TestSkipReasonFallbacksCoverEnumsSkipKeys(t *testing.T) {
	all := enumsStringConstants(t)
	tables := []struct {
		name   string
		prefix string
		table  map[string]string
	}{
		{"BundleMemberSkipFallbacks", "VariantSkip", productenums.BundleMemberSkipFallbacks},
		{"BundleMemberSkipFallbacks", "BundleMember", productenums.BundleMemberSkipFallbacks},
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
			// 反向：表里的 key 必须仍是 enums 里声明的常量（改名后没同步 → 这张表就退化成死条目）。
			// 这张表允许混入业务错误 key（ErrBundleSelfReference 就是一个跳过原因），所以只查「有没有声明」。
			for key := range tc.table {
				if _, ok := all[key]; !ok {
					t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
				}
			}
		})
	}
}

// TestBusinessErrTextKeepsDeclarationFallback 业务错误的成品串来自**声明处**的兜底。
//
// service 用 productenums.Err(key) 返回，兜底随错误经 pkg/facing 带出；出口只读事实
// （pkg/response.businessText），i18n 里没有该词条时退回这句中文 —— 页面上不会出现裸 key。
func TestBusinessErrTextKeepsDeclarationFallback(t *testing.T) {
	c, _ := newHXContext(t, "", "")
	key := productenums.ErrNameRequired
	want := productenums.ProductErrFallbacks[key]
	if want == "" {
		t.Fatalf("%q 在 enums 兜底表里没有登记", key)
	}
	fact, ok := facing.FactOf(productenums.Err(key))
	if !ok || !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v ok=%v", fact, ok)
	}
	if fact.Fallback != want {
		t.Errorf("事实兜底 = %q，want %q（由 productenums.Err 随错误带出）", fact.Fallback, want)
	}
	if got := response.MessageOf(c, "product", productenums.Err(key)); got != want {
		t.Errorf("成品串 = %q，want %q", got, want)
	}
}

// TestBusinessErrDetailStaysInTail 复合错误的明细段不参与取词判定（key 仍可识别）。
//
// service 的复合错误一律 fmt.Errorf("%w: 明细")，判据只看冒号前那一段 —— 明细里带冒号、
// 带中文、带 SQL 都不影响「这是哪条业务错误」。
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
