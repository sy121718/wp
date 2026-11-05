package productenums

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

func errKeyConstants(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	forEachStringConst(t, func(name, val string) {
		if isKeyConstant(name) {
			out[val] = name
		}
	})
	if len(out) == 0 {
		t.Fatal("没扫到任何 Err*/VariantSkip*/BundleMember* 常量 —— 守卫本身失效了")
	}
	return out
}

func isKeyConstant(name string) bool {
	for _, prefix := range []string{"Err", "VariantSkip", "BundleMember"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isErrConstant(name string) bool { return strings.HasPrefix(name, "Err") }

func forEachStringConst(t *testing.T, fn func(name, value string)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
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
					fn(id.Name, val)
				}
			}
		}
	}
}

// TestEveryErrorKeyIsABusinessKey 检查显式错误事实，不用字符串形状决定对外可见性。
func TestEveryErrorKeyIsABusinessKey(t *testing.T) {
	var offenders []string
	for val, name := range errKeyConstants(t) {
		if !isErrConstant(name) {
			continue
		}
		fact, ok := facing.FactOf(Err(val))
		if !ok || !fact.Business || fact.Key != val {
			offenders = append(offenders, fmt.Sprintf("%s (=%q)", name, val))
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("有 %d 条业务错误未通过 Err 声明显式错误事实：\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

func TestErrorKeyValuesAreUnique(t *testing.T) {
	seen := map[string]string{}
	forEachStringConst(t, func(name, val string) {
		if !isErrConstant(name) {
			return
		}
		if prev, dup := seen[val]; dup {
			t.Errorf("取值 %q 被 %s 与 %s 共用 —— 词库按取值索引，两条会互相覆盖", val, prev, name)
		}
		seen[val] = name
	})
}

// TestSkipReasonKeysAreNotErrorDeclarations 守住两张表的边界：
// 跳过原因是回执里的 reasonKey，不是主错误，不该混进 Err* 常量。
func TestSkipReasonKeysAreNotErrorDeclarations(t *testing.T) {
	all := errKeyConstants(t)
	for key := range VariantSkipFallbacks {
		name, ok := all[key]
		if !ok {
			t.Errorf("跳过原因 %q 不在本包常量里（改名后的旧 key？）", key)
			continue
		}
		if isErrConstant(name) {
			t.Errorf("跳过原因 %s 被声明成 Err* 主错误 —— 它只出现在回执的 reasonKey 上", name)
		}
	}
}

func TestFallbackTablesAreDisjoint(t *testing.T) {
	tables := map[string]map[string]string{
		"VariantSkipFallbacks":    VariantSkipFallbacks,
		"ErrDetailFallbacks":      ErrDetailFallbacks,
		"TranslationMsgFallbacks": TranslationMsgFallbacks,
	}
	owner := map[string]string{}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for key := range tables[name] {
			if prev, ok := owner[key]; ok {
				t.Errorf("key %q 同时登记在 %s 与 %s 两张表里", key, prev, name)
			}
			owner[key] = name
		}
	}
}

func TestFallbackTextIsNotTheKey(t *testing.T) {
	tables := map[string]map[string]string{
		"VariantSkipFallbacks":    VariantSkipFallbacks,
		"ErrDetailFallbacks":      ErrDetailFallbacks,
		"TranslationMsgFallbacks": TranslationMsgFallbacks,
	}
	for name, table := range tables {
		for key, text := range table {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s 里 key %q 的兜底是空串（页面上会留下空白错误槽）", name, key)
			}
			if text == key {
				t.Errorf("%s 里 key %q 的兜底就是 key 本身 —— 等于没有兜底", name, key)
			}
		}
	}
}
