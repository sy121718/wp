package producthttp

// product_err_fallback_tables_test.go — 出口层兜底表的守卫 + 「未登记业务 key」的稳定兜底（task-158 §7.3）。
//
// 三张表各有归属，守卫也各在各处：
//   · 本模块 Err* 的展示原文 → internal/module/product/enums 的 ProductErrFallbacks
//     （覆盖面守卫在 productenums 包里）；
//   · 别模块 key（inventory / presentation）→ 本目录的 productErrForeignFallbacks；
//   · 跳过原因（VariantSkip* / BundleMember*）→ variantSkipFallbacks / bundleMemberSkipFallbacks ——
//     它们的取值过不了业务判据（pkg/facing.IsBusinessKey），所以只会从这两张表查，
//     守卫在本文件（漏登一条 = 页面上出现裸 key）。

import (
	"errors"
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
// 跳过原因不走 productErrFactOf（取值过不了业务判据），所以 enums 那层没有它们的表 ——
// 出口层这张表就是唯一真源，漏一条页面上就是裸 key（VariantSkipHasStock 这种串）。
func TestSkipReasonFallbacksCoverEnumsSkipKeys(t *testing.T) {
	all := enumsStringConstants(t)
	tables := []struct {
		name   string
		prefix string
		table  map[string]string
	}{
		{"variantSkipFallbacks", "VariantSkip", variantSkipFallbacks},
		{"bundleMemberSkipFallbacks", "BundleMember", bundleMemberSkipFallbacks},
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

// TestUnregisteredBusinessKeyNeverShowsBareKey 「有业务形态、但没有展示原文」时不许把裸 key 摆到页面上。
//
// 这是 §7.3 ⑥ 的「未知错误保留稳定兜底」：归宿只有两条 —— 登记进表（守卫保证），
// 或者给一句稳定文案 + 一条带 err_key 的 warn 日志。回落成裸 key 是最糟的第三种（运营只会以为后台坏了）。
func TestUnregisteredBusinessKeyNeverShowsBareKey(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	const unregistered = "ErrNotRegisteredInThisRepo"
	msg := productErrMsgQuiet(c, errors.New(unregistered))
	if msg.Key != unregistered {
		t.Fatalf("事实的 key 应保持原样（取词要按它查词条），得到 %q", msg.Key)
	}
	if msg.Fallback == unregistered {
		t.Fatalf("未登记的业务 key 回落成了裸 key：%q", msg.Fallback)
	}
	if msg.Fallback != productErrUnregisteredFallback {
		t.Fatalf("未登记的业务 key 应给稳定兜底 %q，得到 %q", productErrUnregisteredFallback, msg.Fallback)
	}
}

// TestBusinessErrFactKeepsRegisteredTextAndTail 已登记的业务错误：key 原样、兜底取表、明细带出来。
func TestBusinessErrFactKeepsRegisteredTextAndTail(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	err := fmt.Errorf("%s：%s", productenums.ErrNameRequired, "第 3 行")
	fact := productErrFactQuiet(c, err)
	if !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v", fact)
	}
	if fact.Key != productenums.ErrNameRequired {
		t.Fatalf("key = %q，want %q", fact.Key, productenums.ErrNameRequired)
	}
	if fact.Tail != "第 3 行" {
		t.Fatalf("明细 = %q，want %q", fact.Tail, "第 3 行")
	}
	want := productenums.ProductErrFallbacks[productenums.ErrNameRequired]
	if fact.Fallback != want {
		t.Fatalf("兜底 = %q，want %q（由 enums 的表提供）", fact.Fallback, want)
	}
	if msg := productErrMsgQuiet(c, err); msg.Fallback == productenums.ErrNameRequired {
		t.Fatal("业务错误的成品串不许是裸 key")
	}
}

// TestForeignFallbacksDoNotShadowProductKeys 跨模块那张表不许遮住本模块的 key，且没有死条目。
func TestForeignFallbacksDoNotShadowProductKeys(t *testing.T) {
	if len(productErrForeignFallbacks) == 0 {
		t.Fatal("跨模块兜底表空了 —— 若关键条目已搬回各自模块，请连这张表一起删掉")
	}
	for key, text := range productErrForeignFallbacks {
		if strings.TrimSpace(text) == "" {
			t.Errorf("跨模块兜底 %q 是空串", key)
		}
		if text == key {
			t.Errorf("跨模块兜底 %q 就是 key 本身", key)
		}
		if productenums.ProductErrFallbacks[key] != "" {
			t.Errorf("key %q 同时出现在本模块表与跨模块表里（查表顺序会决定文案）", key)
		}
		if got := productErrFallbackText(key); got != text {
			t.Errorf("productErrFallbackText(%q) = %q，want %q", key, got, text)
		}
	}
}

// TestProductErrFallbackTextPrefersEnumsTable 查表先走本模块 enums，再走跨模块那张。
func TestProductErrFallbackTextPrefersEnumsTable(t *testing.T) {
	key := productenums.ErrNameRequired
	want := productenums.ProductErrFallbacks[key]
	if want == "" {
		t.Fatalf("%q 在 enums 兜底表里没有登记", key)
	}
	if got := productErrFallbackText(key); got != want {
		t.Errorf("productErrFallbackText(%q) = %q，want %q", key, got, want)
	}
	if got := productErrFallbackText(""); got != "" {
		t.Errorf("空 key 应返回空串，得到 %q", got)
	}
}
