package commentenums

// comment_enums_test.go — 业务错误的**声明处兜底**与 enums 常量的逐条对账。
//
// 判据：本包每个 Err* 常量都必须在 errFallbacks 里配一句中文，且表里不允许有已删常量的
// 残留。理由是可运营性：兜底跟着 key 住同一个包，加一条业务错误 = 加常量 + 加一句话；
// 忘了一句话的症状是「页面 / 接口上出现裸 key」，不报错、不记日志，只有肉眼能发现。
//
// 原先的白名单 CommentFacingMessages 已删除（那是第二份真相）。判据只剩一份：
// pkg/facing.IsBusinessKey / FactOf。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。
//
// 另外两条钉住「清单类断言」：
//   - 状态白名单与四个常量一一对应（它是**封闭**的：状态机由本模块独占）；
//   - 正文长度上限与迁移 466 的 CHECK 同值（两处不一致时的表现是
//     「service 放行、数据库报错」，而那条报错会被归口成「系统内部错误」）。
//     迁移文件里的字面量没法在 Go 测试里直接读（embed 在 public/migrations），
//     所以这里钉住 Go 侧的值本身，注释指向迁移行号。

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

// fallbackExclusions 不需要兜底的常量 → 理由（必须写明；这张表本身也要能被审计）。
//
// 成功回执（Msg*）不进兜底表：兜底表的语义是「业务错误在词条缺失时显示什么」，
// 而 Msg* 永远不会作为错误响应返回（它们在 response.SuccessWithMessage 那一侧）。
var fallbackExclusions = map[string]string{
	"MsgSubmitPending":  "成功回执：永远不作为错误响应返回",
	"MsgReviewApproved": "成功回执：永远不作为错误响应返回",
	"MsgReviewRejected": "成功回执：永远不作为错误响应返回",
	"MsgPageTitle":      "展示词条（页面标题与菜单标题共用）：只进模板与 sys_menus，不作为响应消息返回",
}

// TestErrFallbacksCoverAllEnums 本包每个 Err* 常量都必须有中文兜底
// （或在 fallbackExclusions 里写明理由），且兜底表里没有死条目。
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
	// 反向：兜底表里的每一条都必须对应一条真实常量（防残留 —— 删了常量却没删兜底，
	// 下一个人会以为那条错误还在用）。
	for key := range errFallbacks {
		if _, ok := declared[key]; !ok {
			t.Errorf("errFallbacks 里的 %q 不对应本包任何常量（已删除？拼写错误？）", key)
		}
	}
}

// TestStatusWhitelistMatchesConstants 状态清单与四个常量一一对应（封闭清单）。
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

// TestReviewableStatusesExcludePending 审核动作只能设「判断结果」两个状态。
func TestReviewableStatusesExcludePending(t *testing.T) {
	if IsReviewableStatus(StatusPending) || IsReviewableStatus(StatusSpam) {
		t.Fatal("审核动作不该接受 pending / spam（它们不是审核判断的结果）")
	}
	if !IsReviewableStatus(StatusApproved) || !IsReviewableStatus(StatusRejected) {
		t.Fatal("通过 / 驳回必须可设")
	}
}

// TestBodyLimitMatchesMigrationCheck 正文上限与迁移 466 的 ck_comments_body_len 同值。
//
// 迁移里写的是 `char_length(body) BETWEEN 1 AND 2000`（466_comments.sql）。
// 两处不一致时的表现是「service 放行、数据库报错」，而那条报错会被归口成
// 「系统内部错误」—— 用户看到的是「评论发不出去」，无从知道是长度问题。
func TestBodyLimitMatchesMigrationCheck(t *testing.T) {
	if MaxBodyLen != 2000 || MinBodyLen != 1 {
		t.Fatalf("正文上限应与迁移 466 的 CHECK 一致（1..2000），实际 %d..%d", MinBodyLen, MaxBodyLen)
	}
	if MaxEntityTypeLen != 40 || MaxEntityIDLen != 64 {
		t.Fatalf("实体类型 / id 长度上限应与迁移 466 的列宽一致（40 / 64），实际 %d / %d",
			MaxEntityTypeLen, MaxEntityIDLen)
	}
}

// TestErrCarriesFallbackAndKey Err(key) 产出的错误同时带 key 与中文兜底
// （判据 pkg/facing.FactOf 的两半），且 errors.Is 仍然认得它。
func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrRateLimited)
	if err.Error() != ErrRateLimited {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrRateLimited, err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != ErrRateLimited || fact.Fallback == "" {
		t.Fatalf("FactOf 应给出业务事实（key + 中文兜底），实际 %+v ok=%v", fact, ok)
	}
	// 非业务错误不该被当成业务错误（否则内部细节会被当作文案透出 —— CQ-010 的教训）。
	if _, ok := facing.FactOf(errors.New("pq: relation comments does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
