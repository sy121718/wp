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

func TestEveryErrorKeyHasFallback(t *testing.T) {
	consts := errKeyConstants(t)

	var missing, forbidden []string
	for val, name := range consts {
		text := strings.TrimSpace(FallbackFor(val))
		if strings.HasPrefix(name, "Err") {
			if text == "" {
				missing = append(missing, fmt.Sprintf("%s (=%q)", name, val))
			}
			continue
		}
		if text != "" {
			forbidden = append(forbidden, fmt.Sprintf("%s (=%q)", name, val))
		}
	}
	sort.Strings(missing)
	sort.Strings(forbidden)
	if len(missing) > 0 {
		t.Errorf("有 %d 条业务错误 key 没有中文兜底，缺词条时页面会显示裸 key：\n  %s\n"+
			"补进 enums.errFallbacks（文案与 seed 的 zh-CN 词条保持一致）",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(forbidden) > 0 {
		t.Errorf("兜底表里登记了「跳过原因」类 key（取值过不了业务判据，永远不会被查到）：%s\n"+
			"它们的兜底表是同目录的 VariantSkipFallbacks（product_skip_fallbacks.go）",
			strings.Join(forbidden, ", "))
	}

	var orphan []string
	for key := range errFallbacks {
		name, ok := consts[key]
		if !ok || !strings.HasPrefix(name, "Err") {
			orphan = append(orphan, key)
		}
	}
	sort.Strings(orphan)
	if len(orphan) > 0 {
		t.Errorf("兜底表里登记了不属于本包 Err* 常量的 key（改名后的旧 key / 别模块的 key）：%s",
			strings.Join(orphan, ", "))
	}
}

func TestErrorKeyPassesBusinessJudgement(t *testing.T) {
	consts := errKeyConstants(t)
	for val, name := range consts {
		if !strings.HasPrefix(name, "Err") {
			continue
		}
		if !facing.IsBusinessKey(val) {
			t.Errorf("%s (=%q) 过不了 pkg/facing.IsBusinessKey —— service 返回它时会被判成内部错误"+
				"（页面/接口一律归口文案），兜底表里也不该有它", name, val)
		}
	}
}

func TestErrorKeyValuesAreUnique(t *testing.T) {
	seen := map[string]string{}
	forEachStringConst(t, func(name, val string) {
		if !strings.HasPrefix(name, "Err") {
			return
		}
		if prev, dup := seen[val]; dup {
			t.Errorf("取值 %q 被 %s 与 %s 共用 —— 兜底表按取值索引，两条会互相覆盖", val, prev, name)
		}
		seen[val] = name
	})
}

func TestFallbackTablesAreDisjoint(t *testing.T) {
	tables := map[string]map[string]string{
		"errFallbacks":            errorFallbackTexts(),
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

func TestFallbackTextsAreReadable(t *testing.T) {
	for key, text := range errorFallbackTexts() {
		if strings.TrimSpace(text) == "" {
			t.Errorf("key %q 的兜底是空串（页面上会留下空白错误槽）", key)
		}
		if text == key {
			t.Errorf("key %q 的兜底就是 key 本身 —— 等于没有兜底，缺词条时照样显示裸 key", key)
		}
		if strings.ContainsAny(text, "{}") {
			t.Errorf("key %q 的兜底含具名占位符（%q）—— 这一层不做填充，占位符会原样摆给运营看", key, text)
		}
	}
}

func errorFallbackTexts() map[string]string {
	texts := make(map[string]string, len(errFallbacks))
	for key, definition := range errFallbacks {
		texts[key] = definition.fallback
	}
	return texts
}
