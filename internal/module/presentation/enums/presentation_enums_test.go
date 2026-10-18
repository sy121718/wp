package presentationenums

// presentation_enums_test.go — 业务错误的**声明处兜底**与 enums 常量的逐条对账。
//
// 判据两条、方向相反：① 每个 Err* 常量都在 errFallbacks 里有非空中文兜底（否则出口只能显示
// 裸 key，例如 ErrPathOccupied）；② 兜底表里没有已删常量的死条目（否则表会退化成注释）。
// 纯 AST 解析，不依赖数据库与装配，毫秒级。
//
// 为什么兜底必须住在声明处：出口只调一次 pkg/facing.FactOf，它给不出兜底时由消费方各补一张
// 补丁表，同一个 key 在不同页面显示不同的话（product 详情模板页就补过两张，随本次迁移删除）。

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

// fallbackExclusions 不需要兜底的常量 → 理由（成功回执永远不作为错误响应返回）。
var fallbackExclusions = map[string]string{
	"MsgCreateSuccess":    "成功回执",
	"MsgRebuildSuccess":   "成功回执",
	"MsgListSuccess":      "成功回执",
	"MsgDetailSuccess":    "成功回执",
	"MsgDeleteSuccess":    "成功回执",
	"MsgPreviewSuccess":   "成功回执",
	"MsgUpdateURLSuccess": "成功回执",
}

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
					declared[value] = name.Name
					if !strings.HasPrefix(name.Name, "Err") {
						continue
					}
					if _, ok := fallbackExclusions[name.Name]; ok {
						continue
					}
					checked++
					if strings.TrimSpace(errFallbacks[value]) == "" {
						t.Errorf("%s = %q 没有中文兜底（在 errFallbacks 里加一句，或在 fallbackExclusions 写明理由）", name.Name, value)
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

// TestErrCarriesFallbackAndKey Err(key) 同时带 key 与中文兜底；ErrDetail 用半角冒号拼明细。
func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrPathOccupied)
	if err.Error() != ErrPathOccupied {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrPathOccupied, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrPathOccupied || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}
	detail := ErrDetail(ErrPathOccupied, "路径 /shop/a 已被占用")
	if !strings.HasPrefix(detail.Error(), ErrPathOccupied+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrPathOccupied || fact.Tail != "路径 /shop/a 已被占用" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation content_templates does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
