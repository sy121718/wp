package feature

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

"go_wp/internal/templates"
"go_wp/internal/text"
"go_wp/internal/adminui"
pageview "go_wp/pkg/view"
"net/url"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

func navBulkOverLimitIDs() []string {
	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "00000000-0000-0000-0000-"+strconv.Itoa(1000000000000+i))
	}
	return ids
}

func navBulkOverLimitJSONBody() string {
	ids := navBulkOverLimitIDs()
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, `"`+id+`"`)
	}
	return `{"ids":[` + strings.Join(quoted, ",") + `]}`
}

func navJSONErrMessage(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, body)
	}
	return resp.Message
}

func navBulkOverLimitRawError() error {
	_, err := bulkids.Normalize(navBulkOverLimitIDs())
	return err
}

func TestNavigationsBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/navigation/delete-bulk", pageview.HTMLFragment, navigationhttp.NewHandle(nil).DeleteBulk)

	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rawErr := navBulkOverLimitRawError()
	if rawErr == nil || !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	form := url.Values{}
	for _, id := range navBulkOverLimitIDs() {
		form.Add("ids", id)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete-bulk",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("超限应回错误片段：%s", body)
	}
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(body, over) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %s，实际 %q", bulkids.MaxIDs, over, body)
	}
	for _, tok := range []string{
		"SQLSTATE", "42P02", "constraint", "uuid", "invalid input syntax", "bulkids:",
	} {
		if strings.Contains(body, tok) {
			t.Errorf("响应泄漏内部细节 %q：%s", tok, body)
		}
	}
}
