package inventoryenums

// inventory_enums_test.go — 业务错误的**声明处兜底**与 enums 常量的逐条对账。
//
// 判据两条、方向相反：① 每个 Err* 常量都在 errFallbacks 里有非空中文兜底（否则页面出口只能
// 把 ErrReceiptOrderDone 这种标识符摆给运营看）；② 兜底表里没有已删常量的死条目。
// 纯 AST 解析，不依赖数据库与装配。
//
// 兜底为什么住在声明处：出口只调一次 pkg/facing.FactOf，它给不出兜底时，消费方只能各补一张
// 补丁表 —— inventory 的页面出口原先就有这样一张（inventoryErrFallbacks，只登记「页面直接
// 铺出来的那几条」），product 也有一张跨模块的（product_foreign_text.go，随本批删除）。
// 两张表都在猜「哪些 key 会露到页面上」，漏登记的方向是静默显示裸 key。

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

// TestErrFallbacksCoverAllEnums 每个 Err* 常量都必须有中文兜底，兜底表里也不能有死条目。
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

// TestErrFallbacksMatchSeededText 已 seed 的 key 在 Go 侧兜底必须与 zh-CN 词条一致。
//
// 两处写同一句话时最容易发生的漂移是「改了一边」，症状是同一条错误在中文站与降级路径上
// 显示两句不同的话（不报错）。这里不读迁移文件（它在 public/migrations，本包不 embed），
// 只钉住几条**页面直接铺出来**的 key 的原文；词条改名时这条测试会先变红。
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

// TestErrCarriesFallbackAndKey Err(key) 同时带 key 与中文兜底；ErrDetail 用半角冒号拼明细。
func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrReceiptOrderDone)
	if err.Error() != ErrReceiptOrderDone {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrReceiptOrderDone, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrReceiptOrderDone || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}
	detail := ErrDetail(ErrReceiptOrderDone, "采购单 PO-1")
	if !strings.HasPrefix(detail.Error(), ErrReceiptOrderDone+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrReceiptOrderDone || fact.Tail != "采购单 PO-1" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation stock_items does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
