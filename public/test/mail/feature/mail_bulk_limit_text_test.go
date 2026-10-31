package feature

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
	"go_wp/internal/shell"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
)

func mailBulkOverLimitForm() url.Values {
	form := url.Values{}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", strconv.Itoa(i+1))
	}
	return form
}

func TestMailAccountsBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := mailhttp.NewMailPageHandle(nil)
	engine.POST("/admin/mail/accounts/bulk-delete", handle.MailAccountsBulkDelete)

	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/mail/accounts/bulk-delete",
		strings.NewReader(mailBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil || !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}

	form := mailBulkOverLimitForm()
	req := httptest.NewRequest(http.MethodPost, "/admin/mail/accounts/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	mailAssertJump(t, rec, false, "一次最多操作")
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) {
		t.Fatalf("受控提示应带上限 %d；正文前 600 字：\n%s", bulkids.MaxIDs, firstN(body, 600))
	}
	if !strings.Contains(body, over) {
		t.Fatalf("受控提示应带本次条数 %s（旧的模块重组实现丢掉的正是这个数）；正文前 600 字：\n%s", over, firstN(body, 600))
	}
	assertMailLeakFree(t, "批量删除超限的提示页", body)
}
