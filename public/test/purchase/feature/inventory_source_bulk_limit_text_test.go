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
	purchasehttp "go_wp/internal/module/purchase/inbound/http"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

func sourceBulkOverLimitForm() url.Values {
	form := url.Values{"projectId": {"00000000-0000-0000-0000-000000000000"}}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", "00000000-0000-0000-0000-"+strconv.Itoa(2000000000000+i))
	}
	return form
}

func TestInventorySourcesBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := purchasehttp.NewSourcePageHandle(nil, nil)
	engine.POST("/admin/inventory/sources/bulk-delete", pageview.HTMLFragment, handle.SourcesBulkDelete)

	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/sources/bulk-delete",
		strings.NewReader(sourceBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := bulkids.Normalize(probe.PostFormArray("ids"))
	if rawErr == nil || !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}

	form := sourceBulkOverLimitForm()
	req := httptest.NewRequest(http.MethodPost, "/admin/inventory/sources/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	assertInventoryJump(t, rec, "err", "一次最多操作",
		strconv.Itoa(bulkids.MaxIDs), strconv.Itoa(bulkids.MaxIDs+1))
	for _, tok := range []string{"SQLSTATE", "constraint", "uq_", "pg_"} {
		if strings.Contains(rec.Body.String(), tok) {
			t.Errorf("提示页泄漏内部细节 %q：%s", tok, rec.Body.String())
		}
	}
}
