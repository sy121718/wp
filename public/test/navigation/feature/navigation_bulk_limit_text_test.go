package feature

// navigation_bulk_limit_text_test.go — 导航菜单页批量删除的 id 超限路径（本批受控类型收口）。
//
// 背景：这一处此前把 berr.Error() 直接拼进 ?err=，靠门禁豁免 + 注释「shell.BulkIDs 的错误是
// 受控中文提示」放行。现在超限错误是**带 sentinel 的类型**（shell.ErrBulkIDsTooMany /
// *shell.BulkIDsError，值域只有 Count/Max 两个整数），页面走 shell.BulkIDsFacingText ——
// 豁免已从 scripts/check-no-internal-error-leak.sh 删除，本文件守住那条收口。
//
// 用 nil service 是刻意的：超限在读到 service 之前就被 shell.BulkIDs 拒绝，
// 这条路径不该碰数据库（碰了就 panic，而不是悄悄跑通）。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	"go_wp/internal/web/shell"
)

// navBulkOverLimitForm 造一份超过 shell.MaxBulkIDs 的批量表单。
func navBulkOverLimitForm() url.Values {
	form := url.Values{"projectId": {"00000000-0000-0000-0000-000000000000"}, "kind": {"header"}}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", "00000000-0000-0000-0000-"+strconv.Itoa(1000000000000+i))
	}
	return form
}

// TestNavigationsBulkDeleteOverLimitUsesControlledText 超限时回带受控文案（两个数字都在），
// 且响应里没有内部错误原文的任何片段。
func TestNavigationsBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := navigationhttp.NewNavigationPageHandle(nil, nil)
	engine.POST("/admin/navigations/bulk-delete", handle.NavigationsBulkDelete)

	// 反证：原始错误是带 sentinel 的类型，值域里只有两个整数。
	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/navigations/bulk-delete",
		strings.NewReader(navBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil || !errors.Is(rawErr, shell.ErrBulkIDsTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}

	form := navBulkOverLimitForm()
	req := httptest.NewRequest(http.MethodPost, "/admin/navigations/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("超限应 303 回列表页，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	rawLocation := rec.Header().Get("Location")
	loc, err := url.Parse(rawLocation)
	if err != nil {
		t.Fatalf("Location 无法解析：%v（%s）", err, rawLocation)
	}
	got := loc.Query().Get("err")
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("应回带受控提示，实际 ?err=%q", got)
	}
	over := strconv.Itoa(shell.MaxBulkIDs + 1)
	if !strings.Contains(got, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(got, over) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %s，实际 ?err=%q", shell.MaxBulkIDs, over, got)
	}
	for _, tok := range []string{"SQLSTATE", "42P02", "constraint", "uuid", "invalid input syntax"} {
		if strings.Contains(rawLocation, tok) {
			t.Errorf("Location 泄漏内部细节 %q：%s", tok, rawLocation)
		}
	}
}
