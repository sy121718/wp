package productenums

// product_err_fallbacks_test.go — 兜底表的**覆盖面**守卫（task-158 §7.3）。
//
// 判据是**双向**的，两个方向都判失败：
//   · 本包声明了一条 Err* 常量却没有登记兜底 → 失败（缺词条时页面会出现裸 key，
//     而这正是「加业务错误」最容易漏的一步）；
//   · 表里登记了一个不属于本包的 key（已改名的旧 key / 别模块的 key）→ 失败
//     （登记表退化成注释就没人看它了，与 rawSQLDebtList / controller-i18n 白名单同一判据）。
//
// 为什么 VariantSkip* 反过来要求「不许登记」：它们是「这一行被跳过」的原因，
// 不是错误消息 —— 取值过不了 pkg/facing.IsBusinessKey（判据只认 Err*/Msg* 与点分 key 两种形态），
// 所以经 productErrFactOf 永远拿不到，登记了就是死条目。它们的兜底表是本目录的
// VariantSkipFallbacks（product_skip_fallbacks.go），由同名 _test 守卫。
//
// 表本身不参与取词优先级（先查 sys_i18n 词条），所以「多登记」不影响行为 —— 能变红的只有
// 「漏登记」与「登记了不存在 / 不该登记的 key」，这两件都不该靠人眼守。

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

// errKeyConstants 扫本包全部非测试源码里的 key 类常量：取值 → 常量名。
//
// 用 AST 而不是正则：常量块里的对齐、注释、跨行拼接都不影响取值，改名重构也不会让守卫静默失效。
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

// forEachStringConst 遍历本包非测试源文件里 `Name = "字面量"` 形态的常量。
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

// TestEveryErrorKeyHasFallback 每条业务错误 key 都要有展示兜底，且表里不许混进「不是错误」的 key。
func TestEveryErrorKeyHasFallback(t *testing.T) {
	consts := errKeyConstants(t)

	var missing, forbidden []string
	for val, name := range consts {
		text := strings.TrimSpace(ProductErrFallbacks[val])
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
			"补进 enums.ProductErrFallbacks（文案与 seed 的 zh-CN 词条保持一致）",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(forbidden) > 0 {
		t.Errorf("兜底表里登记了「跳过原因」类 key（取值过不了业务判据，永远不会被查到）：%s\n"+
			"它们的兜底表是同目录的 VariantSkipFallbacks（product_skip_fallbacks.go）",
			strings.Join(forbidden, ", "))
	}

	var orphan []string
	for key := range ProductErrFallbacks {
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

// TestErrorKeyPassesBusinessJudgement 错误 key 必须过**共享**的业务判据。
//
// 判据只有一份（pkg/facing.IsBusinessKey，pkg/response.IsBusinessError 转发它）：装一条过不了
// 判据的错误 key，service 返回它时会被各路出口判成内部错误 —— 那种 key 要么改形态，要么去出口层放。
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

// TestErrorKeyValuesAreUnique 同一个取值不能被两条常量共用。
//
// 兜底表按**取值**（= sys_i18n 的 item_key）索引：两条常量取值相同而文案不同时，map 会静默
// 只留一条（Go 允许重复取值的常量）。
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

// TestFallbackTablesAreDisjoint 三张展示兜底表不许有交集。
//
// 同一条 key 在两张表里注册两句话时，页面显示哪句由「哪张表先被查」决定 —— 那属于实现细节，
// 不该由它决定文案。
func TestFallbackTablesAreDisjoint(t *testing.T) {
	tables := map[string]map[string]string{
		"ProductErrFallbacks":     ProductErrFallbacks,
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

// TestFallbackTextsAreReadable 兜底原文必须是一句可读的话，而不是裸 key 的副本。
func TestFallbackTextsAreReadable(t *testing.T) {
	for key, text := range ProductErrFallbacks {
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
