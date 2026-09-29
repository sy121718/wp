package commentenums

// comment_enums_test.go — 对外文案白名单（CommentFacingMessages）与 enums 常量的逐条对账。
//
// 为什么要这条测试：白名单是 handler 归口助手（inbound/http/comment_err.go）与
// 契约出口（service.FacingText）共同的判据 —— **漏写一条**的后果是「该业务错误在页面上
// 显示成一句通用提示」：功能不算坏，但没人会发现，而新增业务文案恰恰是每次改需求都会发生的事；
// **多写一条**没有后果。所以把「本包有多少条对外错误文案常量」与「白名单里有几条」绑在一起：
// 新增一条 Err* 却忘了进白名单，这里直接变红。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。形态照 membership/enums 的同名测试。
//
// 另外两条钉住「清单类断言」：
//   - 状态白名单与四个常量一一对应（它是**封闭**的：状态机由本模块独占）；
//   - 正文长度上限与迁移 466 的 CHECK 同值（两处不一致时的表现是
//     「service 放行、数据库报错」，而那条报错会被归口成「系统内部错误」）。
//     迁移文件里的字面量没法在 Go 测试里直接读（embed 在 public/migrations），
//     所以这里钉住 Go 侧的值本身，注释指向迁移行号。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// messageExclusions 不进白名单的常量 → 理由（必须写明；这张表本身也要能被审计）。
//
// 成功回执（Msg*）不进白名单：白名单的语义是「未命中就记日志 + 归口」，
// 而 Msg* 永远不会作为错误响应返回（它们在 response.SuccessWithMessage 那一侧）。
var messageExclusions = map[string]string{
	"MsgSubmitPending":  "成功回执：永远不作为错误响应返回",
	"MsgReviewApproved": "成功回执：永远不作为错误响应返回",
	"MsgReviewRejected": "成功回执：永远不作为错误响应返回",
	"MsgPageTitle":      "展示词条（页面标题与菜单标题共用）：只进模板与 sys_menus，不作为响应消息返回",
	"ErrInternal":       "归口文案本身：它是未命中白名单时的返回值，不是业务文案",
}

// TestCommentFacingMessagesCoverAllEnums 本包每个 Err* 常量都必须落在白名单里
// （或在 messageExclusions 里写明理由），且白名单里的值都必须真的是一条常量。
func TestCommentFacingMessagesCoverAllEnums(t *testing.T) {
	inWhitelist := make(map[string]bool, len(CommentFacingMessages))
	for _, v := range CommentFacingMessages {
		inWhitelist[v] = true
	}

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
					if inWhitelist[value] {
						continue
					}
					if _, ok := messageExclusions[name.Name]; ok {
						continue
					}
					t.Errorf("%s = %q 既不在 CommentFacingMessages 里，也没有在 messageExclusions 写明理由", name.Name, value)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何 Err* / Msg* 常量：判据失效（源码结构变了？）")
	}
	// 反向：白名单里的每一条都必须对应一条真实常量（防错别字 —— 写错一个字符的白名单条目
	// 不会命中任何错误，于是那条业务文案永远走归口分支，且没有任何迹象）。
	for _, v := range CommentFacingMessages {
		if _, ok := declared[v]; !ok {
			t.Errorf("白名单里的 %q 不对应本包任何 Err* / Msg* 常量（拼写错误？）", v)
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

// TestHitFacingMessageIsExactMatch 白名单判定只认整串相等（防「拼过的串混进来」）。
func TestHitFacingMessageIsExactMatch(t *testing.T) {
	if _, ok := HitFacingMessage(ErrRateLimited); !ok {
		t.Fatal("白名单里的值必须命中")
	}
	// 带前缀 / 后缀的串**不该**命中：命中白名单意味着「可以原样展示给前端」，
	// 而拼过的串可能夹带内部细节（CQ-010 的教训）。
	for _, bad := range []string{
		ErrRateLimited + "：user 1024",
		"prefix " + ErrRateLimited,
		`pq: relation "comments" does not exist`,
		"",
	} {
		if _, ok := HitFacingMessage(bad); ok {
			t.Errorf("%q 不该命中白名单", bad)
		}
	}
}
