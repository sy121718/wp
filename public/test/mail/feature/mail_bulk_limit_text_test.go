package feature

// mail_bulk_limit_text_test.go — 邮箱后台批量删除的 id 超限路径（本批受控类型收口）。
//
// 背景：mailBulkIDsText 此前用 shell.MaxBulkIDs **重新组织**了同一句话 —— 副作用是丢掉
// 「当前 N 项」（去重后的条数只有 shell 知道，在模块里重算就是第二份真相）。现在它只转调
// shell.BulkIDsFacingText，超限错误是带 sentinel 的类型（shell.ErrBulkIDsTooMany /
// *shell.BulkIDsError），两个数字都由类型给回。
//
// 用 nil service：超限在读到 service 之前就被 shell.BulkIDs 拒绝，这条路径不碰数据库。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/web/shell"
)

// mailBulkOverLimitForm 造一份超过 shell.MaxBulkIDs 的批量表单（发信账号批量删除）。
func mailBulkOverLimitForm() url.Values {
	form := url.Values{}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", strconv.Itoa(i+1))
	}
	return form
}

// TestMailAccountsBulkDeleteOverLimitUsesControlledText 超限时 302 回带受控文案，
// 且**「当前 M 项」确实回来了** —— 这条正是本批对 mail/order/user 的目的。
func TestMailAccountsBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := mailhttp.NewMailPageHandle(nil)
	engine.POST("/admin/mail/accounts/bulk-delete", handle.MailAccountsBulkDelete)

	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/mail/accounts/bulk-delete",
		strings.NewReader(mailBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil || !errors.Is(rawErr, shell.ErrBulkIDsTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}

	form := mailBulkOverLimitForm()
	req := httptest.NewRequest(http.MethodPost, "/admin/mail/accounts/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("超限应 302 回列表页，实际 %d，body=%s", rec.Code, rec.Body.String())
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
	if !strings.Contains(got, strconv.Itoa(shell.MaxBulkIDs)) {
		t.Fatalf("受控提示应带上限 %d，实际 ?err=%q", shell.MaxBulkIDs, got)
	}
	if !strings.Contains(got, over) {
		t.Fatalf("受控提示应带本次条数 %s（旧的模块重组实现丢掉的正是这个数），实际 ?err=%q", over, got)
	}
	assertMailLeakFree(t, "批量删除超限的 ?err=", got)
}
