package commentenums

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
	"MsgSubmitPending":  "成功回执：永远不作为错误响应返回",
	"MsgReviewApproved": "成功回执：永远不作为错误响应返回",
	"MsgReviewRejected": "成功回执：永远不作为错误响应返回",
	"MsgPageTitle":      "展示词条（页面标题与菜单标题共用）：只进模板与 sys_menus，不作为响应消息返回",
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

func TestStatusWhitelistMatchesConstants(t *testing.T) {
	got := Statuses()
	want := []string{StatusPending, StatusApproved, StatusRejected, StatusSpam}
	if len(got) != len(want) {
		t.Fatalf("Statuses 长度应为 %d，实际 %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Statuses[%d] = %q，期望 %q", i, got[i], want[i])
		}
		if !IsValidStatus(want[i]) {
			t.Errorf("%q 必须被判为合法状态", want[i])
		}
	}
	for _, bad := range []string{"", "PENDING", "deleted", "pending "} {
		if IsValidStatus(bad) {
			t.Errorf("%q 不该被判为合法状态", bad)
		}
	}
}

func TestReviewableStatusesExcludePending(t *testing.T) {
	if IsReviewableStatus(StatusPending) || IsReviewableStatus(StatusSpam) {
		t.Fatal("审核动作不该接受 pending / spam（它们不是审核判断的结果）")
	}
	if !IsReviewableStatus(StatusApproved) || !IsReviewableStatus(StatusRejected) {
		t.Fatal("通过 / 驳回必须可设")
	}
}

func TestBodyLimitMatchesMigrationCheck(t *testing.T) {
	if MaxBodyLen != 2000 || MinBodyLen != 1 {
		t.Fatalf("正文上限应与迁移 466 的 CHECK 一致（1..2000），实际 %d..%d", MinBodyLen, MaxBodyLen)
	}
	if MaxEntityTypeLen != 40 || MaxEntityIDLen != 64 {
		t.Fatalf("实体类型 / id 长度上限应与迁移 466 的列宽一致（40 / 64），实际 %d / %d",
			MaxEntityTypeLen, MaxEntityIDLen)
	}
}

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrRateLimited)
	if err.Error() != ErrRateLimited {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrRateLimited, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrRateLimited || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}

	if _, ok := facing.FactOf(errors.New("pq: relation comments does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
