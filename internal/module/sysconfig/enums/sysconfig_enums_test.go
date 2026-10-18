package sysconfigenums

// sysconfig_enums_test.go — 业务错误**声明处兜底**与 enums 常量的逐条对账。
//
// 判据：本包每一个 Err* 常量都必须在 errFallbacks 里有一句中文兜底，且表里不允许有
// 已删除常量的残留。理由是二开：兜底跟着 key 住在同一个包，加一条业务错误 = 加常量 +
// 加一句话，忘了一句话的症状是「页面上显示裸 key」—— 不报错、不记日志，只有肉眼能发现。
//
// 原先的 SysConfigFacingMessages 白名单已删除（那是第二份真相：service 换措辞、或某个 key
// 改由别处产生时就静默失配）。可透出性判据只有一份，在 pkg/facing。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fallbackExclusions 不需要兜底的常量 → 理由。
var fallbackExclusions = map[string]string{
	"MsgGroupSaved":    "成功回执：永远不作为错误响应返回",
	"MsgListSuccess":   "成功回执：永远不作为错误响应返回",
	"MsgDetailSuccess": "成功回执：永远不作为错误响应返回",
}

// TestErrFallbacksCoverAllEnums 每个 Err* 常量都要有中文兜底，且表里没有死条目。
func TestErrFallbacksCoverAllEnums(t *testing.T) {
	declared := map[string]string{} // 常量值 → 常量名
	for _, file := range enumFiles(t) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					continue
				}
				declared[val] = vs.Names[0].Name
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("没有解析到任何字符串常量：AST 收集逻辑失效，这条门禁会一直绿")
	}

	checked := 0
	for val, name := range declared {
		if !strings.HasPrefix(name, "Err") {
			continue
		}
		if _, excluded := fallbackExclusions[name]; excluded {
			continue
		}
		checked++
		if strings.TrimSpace(errFallbacks[val]) == "" {
			t.Errorf("常量 %s 没有中文兜底（在 errFallbacks 里加一句，或写进 fallbackExclusions 并说明理由）", name)
		}
	}
	if checked == 0 {
		t.Fatal("没有检查到任何 Err* 常量：筛选逻辑失效")
	}
	for key := range errFallbacks {
		if _, ok := declared[key]; !ok {
			t.Errorf("errFallbacks 里的 %q 不是本包的常量值（错别字 / 已删除的常量）", key)
		}
	}
}

// TestErrCarriesFallback Err(key) 产出的错误自带走查判据需要的两半（key + 中文兜底）。
func TestErrCarriesFallback(t *testing.T) {
	for _, key := range []string{ErrInvalidParam, ErrVersionRequired, ErrGroupNotFound, ErrVersionConflict} {
		err := Err(key)
		if err.Error() != key {
			t.Errorf("Err(%q).Error() = %q，want 原 key", key, err.Error())
		}
		if got := errFallbacks[key]; got == "" {
			t.Errorf("%q 缺兜底", key)
		}
	}
	// 未登记的 key 也必须是合法错误：兜底回落成 key，而不是空串（空串会让失败在页面上不可见）。
	if err := Err("sysconfig.err.something_new"); err == nil || err.Error() == "" {
		t.Fatal("未登记的 key 也必须给出非空错误")
	}
}

// enumFiles 本包的非测试源文件。
func enumFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		out = append(out, f)
	}
	return out
}
