package inventoryenums

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
		parsed, perr := parser.ParseFile(token.NewFileSet(), file, nil, 0)
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
					if !strings.HasPrefix(name.Name, "Err") || i >= len(vs.Values) {
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
					declared[value] = name.Name
					checked++
					if strings.TrimSpace(errFallbacks[value]) == "" {
						t.Errorf("%s = %q 没有中文兜底（在 errFallbacks 里加一句）", name.Name, value)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何 Err* 常量：判据失效（源码结构变了？）")
	}
	for key := range errFallbacks {
		if _, ok := declared[key]; !ok {
			t.Errorf("errFallbacks 里的 %q 不对应本包任何常量（已删除？拼写错误？）", key)
		}
	}
}

func TestErrFallbacksMatchSeededText(t *testing.T) {
	cases := map[string]string{
		ErrWarehouseNotFound:     "仓库不存在",
		ErrWarehouseNameRequired: "仓库名称必填",
		ErrExternalSKUInvalid:    "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	}
	for key, want := range cases {
		if got := errFallbacks[key]; got != want {
			t.Errorf("%q 兜底 = %q，want %q（与 sys_i18n 的 zh-CN 词条同句）", key, got, want)
		}
	}
}

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrWarehouseNotFound)
	if err.Error() != ErrWarehouseNotFound {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrWarehouseNotFound, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrWarehouseNotFound || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}
	detail := ErrDetail(ErrWarehouseNotFound, "仓库 WH-1")
	if !strings.HasPrefix(detail.Error(), ErrWarehouseNotFound+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrWarehouseNotFound || fact.Tail != "仓库 WH-1" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation stock_items does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
