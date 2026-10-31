package membershipenums

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

var fallbackExclusions = map[string]string{
	"MsgTierCreated":      "成功回执：永远不作为错误响应返回",
	"MsgTierUpdated":      "成功回执：永远不作为错误响应返回",
	"MsgTierDeleted":      "成功回执：永远不作为错误响应返回",
	"MsgEntitlementSaved": "成功回执：永远不作为错误响应返回",
	"MsgAssignSet":        "成功回执：永远不作为错误响应返回",
	"MsgAssignUnlocked":   "成功回执：永远不作为错误响应返回",
}

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

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrDefaultTierMissing)
	if err.Error() != ErrDefaultTierMissing {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf(Err(...)) 未判成业务错误: %+v, %v", fact, ok)
	}
	if fact.Fallback == "" || fact.Fallback == fact.Key {
		t.Errorf("兜底为空或与 key 同形（出口会显示裸 key）: %+v", fact)
	}
	if fact.Tail != "" {
		t.Errorf("不带定位信息的错误不该有 Tail: %+v", fact)
	}

	if got := Err("membership.err.notDeclared"); got.Error() != "membership.err.notDeclared" {
		t.Errorf("未登记 key 的 Error() = %q", got.Error())
	}

	if _, ok := facing.FactOf(errors.New(`
pq: relation "membership_tiers" does not exist`)); ok {
		t.Error("数据库原文不该被判成业务错误")
	}
}

func TestErrDetailKeepsTail(t *testing.T) {
	err := ErrDetail(ErrDefaultTierMissing, "p-1")
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf(ErrDetail(...)) 未判成业务错误: %+v, %v", fact, ok)
	}
	if fact.Key != ErrDefaultTierMissing || fact.Tail != "p-1" {
		t.Fatalf("Fact = %+v，应保留 key 与定位信息", fact)
	}
	if fact.Fallback == "" || fact.Fallback == fact.Key {
		t.Errorf("带定位信息的错误也要带兜底: %+v", fact)
	}

	if ErrDetail(ErrNotFound, "   ").Error() != ErrNotFound {
		t.Errorf("空定位信息不该拼分隔符: %q", ErrDetail(ErrNotFound, "   ").Error())
	}
}

func TestEntitlementKindsMatchConstants(t *testing.T) {
	if len(EntitlementKinds) != 2 {
		t.Fatalf("EntitlementKinds = %v，本期恰好两种", EntitlementKinds)
	}
	if EntitlementKinds[0] != KindFreeShipping || EntitlementKinds[1] != KindDiscount {
		t.Errorf("EntitlementKinds = %v", EntitlementKinds)
	}
}
