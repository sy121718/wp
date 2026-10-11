package pagehttp

// page_bulk_notice_test.go — 批量结论的**写侧形状**回归（纯逻辑，不需要数据库）。
//
// 历史上这里测的是「受控回执的防伪造」（URL 里的文案不可伪造、计数有上限）—— 那套机制
// 随「结论走 shell.RenderJump」整批删除：文案现在直接渲染进响应体，不可信输入不在这条链上。
// 保留下来的是**结论本身**的判据：四个分支都要给出非空、含计数的整句，取词失败不能让
// 提示页显示一句空话（用户会以为操作成功了）。
//
// task-152 起结论是**事实**（pageMsg：key + 中文兜底 + `%s` 参数），不再是拼好的字符串 ——
// 所以这里同时钉住三件事：① key 非空（语言可切换）；② 兜底含 `%s`（缺词条时不会显示裸 key）；
// ③ 渲染出来的整句含计数。
//
// 上限拒绝的文案由 adminui.BulkIDsFacingText 提供（类型判定，不认文本），其回归在
// internal/adminui/bulkids_test.go；这里补一条「page 的批量入口拿到的是同一条受控文案」。

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	bulkids "go_wp/pkg/bulkids"
)

func noticeCtx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/admin/pages?"+rawQuery, nil)
	return c
}

// TestBulkDeleteResultBranches 页面列表批量删除的四个结论分支都给出可切换语言、含计数的整句。
func TestBulkDeleteResultBranches(t *testing.T) {
	c := noticeCtx("")
	for _, tc := range []struct {
		name             string
		deleted, skipped int
		wantCount        string
	}{
		{"未勾选", 0, 0, ""},
		{"全成功", 3, 0, "3"},
		{"全部失败", 0, 3, "3"},
		{"部分成功", 2, 3, "2"},
	} {
		msg := outcomeMsg(pageenums.PagesBulkDeleteOutcome(tc.deleted, tc.skipped))
		if strings.TrimSpace(msg.Key) == "" {
			t.Fatalf("%s：结论没有 i18n key（提示页会永远停在中文）", tc.name)
		}
		got := pageMsgText(c, msg)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("%s：批量结论为空串（提示页会显示一句空话）", tc.name)
		}
		if tc.wantCount != "" && !strings.Contains(got, tc.wantCount) {
			t.Fatalf("%s：结论应含计数 %q，实际 %q", tc.name, tc.wantCount, got)
		}
	}
}

// TestRedirectBulkDeleteResultBranches 重定向批量删除的四个分支同样给出非空整句。
func TestRedirectBulkDeleteResultBranches(t *testing.T) {
	c := noticeCtx("")
	for _, tc := range []struct {
		name             string
		deleted, skipped int
	}{
		{"未勾选", 0, 0}, {"全成功", 3, 0}, {"全部失败", 0, 3}, {"部分成功", 2, 3},
	} {
		msg := outcomeMsg(pageenums.RedirectBulkDeleteOutcome(tc.deleted, tc.skipped))
		if strings.TrimSpace(msg.Key) == "" {
			t.Fatalf("%s：重定向批量结论没有 i18n key", tc.name)
		}
		if got := pageMsgText(c, msg); strings.TrimSpace(got) == "" {
			t.Fatalf("%s：重定向批量结论为空串", tc.name)
		}
	}
}

// TestBulkIDsFacingTextIsControlled 上限拒绝走受控文案出口：命中给可行动数字，
// 未命中回落归口文案，绝不透出原文。
func TestBulkIDsFacingTextIsControlled(t *testing.T) {
	c := noticeCtx("")
	tooMany := &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs}
	got := adminui.BulkIDsFacingText(c, tooMany)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("上限拒绝应给可行动文案，实际 %q", got)
	}
	unknown := adminui.BulkIDsFacingText(c, errors.New("boom: relation \"pages\" does not exist (SQLSTATE 42P01)"))
	if strings.Contains(unknown, "boom") || strings.Contains(unknown, "SQLSTATE") {
		t.Fatalf("未命中的错误不该透出原文，实际 %q", unknown)
	}
}

// TestPageBulkIDsMsgIsAFact 提示页那条超限结论同样是事实：key 非空、两个数字经 withArgs 带上。
func TestPageBulkIDsMsgIsAFact(t *testing.T) {
	c := noticeCtx("")
	tooMany := &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs}
	msg := pageBulkIDsMsg(c, tooMany)
	if msg.Key != bulkids.MsgTooMany {
		t.Fatalf("超限结论应带 bulkids.MsgTooMany，实际 %q", msg.Key)
	}
	if len(msg.Args) != 2 {
		t.Fatalf("超限结论应带两个参数（上限、本次条数），实际 %v", msg.Args)
	}
	if got := pageMsgText(c, msg); !strings.Contains(got, msg.Args[1]) {
		t.Fatalf("渲染结果应含本次条数 %q，实际 %q", msg.Args[1], got)
	}
	// 不认识的错误 → 归口，不透出原文。
	other := pageBulkIDsMsg(c, errors.New("boom: SQLSTATE 42P01"))
	if strings.Contains(pageMsgText(c, other), "boom") {
		t.Fatalf("未命中的错误不该透出原文：%q", pageMsgText(c, other))
	}
}
