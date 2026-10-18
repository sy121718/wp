package contentenums

// content_enums_test.go — 错误兜底表（errFallbacks）与 enums 常量的逐条对账。
//
// 兜底跟着**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。漏写一条兜底时
// 页面上会出现裸 key 或一句通用提示 —— 功能不算坏，但没人会发现，而新增业务文案恰恰是
// 每次改需求都会发生的事。
//
// 双向判失败：新增 Err* 却没给兜底要红；兜底表里留着已删除的 key 同样要红。
// 纯 AST 解析，形态照 membership / comment / sysconfig / navigation / analytics 的同名测试。

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

// fallbackExclusions 不进兜底表的常量 → 理由（必须写明；这张表本身也要能被审计）。
//
// 成功回执（Msg*）不进：兜底表只服务**错误**出口，Msg* 走 response.SuccessWithMessage。
var fallbackExclusions = map[string]string{
	"MsgCreateSuccess":      "成功回执：永远不作为错误响应返回",
	"MsgUpdateSuccess":      "成功回执：永远不作为错误响应返回",
	"MsgListSuccess":        "成功回执：永远不作为错误响应返回",
	"MsgDetailSuccess":      "成功回执：永远不作为错误响应返回",
	"MsgDeleteSuccess":      "成功回执：永远不作为错误响应返回",
	"MsgCollectionsSuccess": "成功回执：永远不作为错误响应返回",

	// 文章页的**参数级提示**（点分 key，不是 facing 哨兵）：值即 sys_i18n 的 item_key，
	// 兜底写在页面出口的 articleFallbacks 里（导入 / 发布 / 必填提示各自场景一句话），
	// 不经 pkg/facing 的判据，因此不进 errFallbacks。
	"ErrMissingID":    "参数级提示 key（非哨兵错误），兜底在页面出口的 articleFallbacks",
	"ErrMissingTitle": "参数级提示 key（非哨兵错误），兜底在页面出口的 articleFallbacks",
	"ErrMissingSlug":  "参数级提示 key（非哨兵错误），兜底在页面出口的 articleFallbacks",
}

// TestErrFallbacksCoverAllEnums 每个 Err* 常量都必须有非空兜底（或写明豁免），
// 且兜底表的每个 key 都对应一条真实常量。
func TestErrFallbacksCoverAllEnums(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	declared := map[string]string{}
	checked := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", file, perr)
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
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Err") && !strings.HasPrefix(name.Name, "Msg") {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", file, name.Name, uerr)
					}
					checked++
					declared[value] = name.Name
					if _, ok := fallbackExclusions[name.Name]; ok {
						continue
					}
					if !strings.HasPrefix(name.Name, "Err") {
						t.Errorf("%s 不是 Err* 常量：Msg* 应写进 fallbackExclusions 并说明理由", name.Name)
						continue
					}
					if strings.TrimSpace(errFallbacks[value]) == "" {
						t.Errorf("%s = %q 没有在 errFallbacks 里给中文兜底", name.Name, value)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何 Err* / Msg* 常量：判据失效（源码结构变了？）")
	}
	for value := range errFallbacks {
		if _, ok := declared[value]; !ok {
			t.Errorf("errFallbacks 里的 %q 不对应本包任何 Err* 常量（拼写错误或常量已删？）", value)
		}
	}
}

// TestErrCarriesFallbackAndKey Err 工厂把「key + 中文兜底」一起交给 pkg/facing 的判据。
func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrNotFound)
	if err.Error() != ErrNotFound {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf 应判为业务错误，得到 %+v / %v", fact, ok)
	}
	if fact.Fallback == "" || fact.Fallback == fact.Key {
		t.Errorf("兜底不该为空、也不该回落成 key 本身：%+v", fact)
	}
}

// TestErrDetailKeepsTail 定位信息留在 Tail 里，分隔符只能是 ASCII「: 」；空明细退化成纯 key。
func TestErrDetailKeepsTail(t *testing.T) {
	fact, ok := facing.FactOf(ErrDetail(ErrInvalidField, "`body`"))
	if !ok || !fact.Business || fact.Key != ErrInvalidField {
		t.Fatalf("带明细的业务错误应判为业务事实，得到 %+v / %v", fact, ok)
	}
	if fact.Tail != "`body`" {
		t.Errorf("Tail = %q，应为定位信息", fact.Tail)
	}
	if got := ErrDetail(ErrInvalidField, "  ").Error(); got != ErrInvalidField {
		t.Errorf("空明细应退化成纯 key，实际 %q", got)
	}
	// 内部原文（表名 / SQLSTATE）不能被判成业务错误。
	if _, ok := facing.FactOf(errors.New(`pq: relation "contents" does not exist`)); ok {
		t.Error("内部原文不该被判成业务事实")
	}
}
