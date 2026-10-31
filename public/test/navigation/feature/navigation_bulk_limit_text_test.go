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

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	bulkids "go_wp/pkg/bulkids"
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

	engine.POST("/api/navigation/delete", navigationhttp.NewHandle(nil).Delete)

	rawErr := navBulkOverLimitRawError()
	if rawErr == nil || !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete",
		strings.NewReader(navBulkOverLimitJSONBody()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	message := navJSONErrMessage(t, body)
	if !strings.HasPrefix(message, bulkids.MsgTooMany+"|") {
		t.Fatalf("超限应回受控 key（%s）的带参协议而不是原文，实际 %q", bulkids.MsgTooMany, message)
	}
	if !strings.Contains(message, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(message, over) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %s，实际 %q", bulkids.MaxIDs, over, message)
	}
	for _, tok := range []string{
		"SQLSTATE", "42P02", "constraint", "uuid", "invalid input syntax", "bulkids:",
	} {
		if strings.Contains(body, tok) {
			t.Errorf("响应泄漏内部细节 %q：%s", tok, body)
		}
	}
}
