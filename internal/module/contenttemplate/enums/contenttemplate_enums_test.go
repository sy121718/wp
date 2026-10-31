package contenttemplateenums

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

func TestNotFoundDeclares404(t *testing.T) {
	for _, err := range []error{Err(ErrNotFound), ErrDetail(ErrNotFound, "模板 tpl-1")} {
		if status, ok := facing.StatusOf(err); !ok || status != http.StatusNotFound {
			t.Fatalf("模板不存在应携带 404，实际 status=%d ok=%v err=%v", status, ok, err)
		}
	}
}

var fallbackExclusions = map[string]string{
	"MsgCreateSuccess": "成功回执",
	"MsgUpdateSuccess": "成功回执",
	"MsgListSuccess":   "成功回执",
	"MsgDetailSuccess": "成功回执",
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

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrNotFound)
	if err.Error() != ErrNotFound {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrNotFound, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrNotFound || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}
	detail := ErrDetail(ErrNotFound, "模板 tpl-1")
	if !strings.HasPrefix(detail.Error(), ErrNotFound+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrNotFound || fact.Tail != "模板 tpl-1" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation content_templates does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
