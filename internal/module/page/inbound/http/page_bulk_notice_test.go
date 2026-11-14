package pagehttp

import (
	"errors"
	"go_wp/pkg/response"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	pageenums "go_wp/internal/module/page/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

func noticeCtx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/admin/pages?"+rawQuery, nil)
	return c
}

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

func TestBulkIDsFacingTextIsControlled(t *testing.T) {
	// 超限文案归词库：不注入时 BulkIDsFacingText 只能给归口文案，断言里的
	// 「一次最多操作」就成了没来源的硬编码期望。注入值直接引用生产模板常量，
	// 与 public/migrations/277_i18n_seed_shell_bulk_ids.sql 的 zh-CN 值一致。
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := noticeCtx("")
	tooMany := &bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs}
	got := response.MessageOf(c, "page", tooMany)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("上限拒绝应给可行动文案，实际 %q", got)
	}
	unknown := response.MessageOf(c, "page", errors.New("boom: relation \"pages\" does not exist (SQLSTATE 42P01)"))
	if strings.Contains(unknown, "boom") || strings.Contains(unknown, "SQLSTATE") {
		t.Fatalf("未命中的错误不该透出原文，实际 %q", unknown)
	}
}

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

	other := pageBulkIDsMsg(c, errors.New("boom: SQLSTATE 42P01"))
	if strings.Contains(pageMsgText(c, other), "boom") {
		t.Fatalf("未命中的错误不该透出原文：%q", pageMsgText(c, other))
	}
}
