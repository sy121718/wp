package feature

import (
	"bytes"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationenums "go_wp/internal/module/navigation/enums"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
)

const navInternalKey = "navigation.err.internal"

var internalLeakTokens = []string{
	"SQLSTATE", "uq_", "pg_", "navigations", "constraint", "uuid", "invalid input syntax",
}

func assertNoInternalLeak(t *testing.T, body string) {
	t.Helper()
	for _, tok := range internalLeakTokens {
		if strings.Contains(body, tok) {
			t.Errorf("响应体泄漏内部细节 %q: %s", tok, body)
		}
	}
}

func newNavErrContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	return c, w
}

func TestNavigationListHidesInternalError(t *testing.T) {
	_, _, navSvc, _ := newNavigationEnv(t)
	h := navigationhttp.NewHandle(navSvc)

	c, w := newNavErrContext(http.MethodGet, "/api/navigation/list?projectId=not-a-uuid&kind=header", nil)
	h.List(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非法工程 id 属内部错误，状态码应为 500，实际 %d，body=%s", w.Code, w.Body.String())
	}
	assertNoInternalLeak(t, w.Body.String())
	if !strings.Contains(w.Body.String(), navInternalKey) {
		t.Errorf("响应体应返回归口文案 %q，实际 %s", navInternalKey, w.Body.String())
	}
}

func TestNavigationCreateHidesInternalError(t *testing.T) {
	_, _, navSvc, _ := newNavigationEnv(t)
	h := navigationhttp.NewHandle(navSvc)

	reqBody, err := json.Marshal(map[string]any{
		"projectId": "not-a-uuid", "title": "首页", "path": "/", "kind": "header",
	})
	if err != nil {
		t.Fatalf("构造请求体失败: %v", err)
	}
	c, w := newNavErrContext(http.MethodPost, "/api/navigation/create", reqBody)
	h.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法工程 id 应在参数校验处被拒（400），实际 %d，body=%s", w.Code, w.Body.String())
	}
	assertNoInternalLeak(t, w.Body.String())
}

func TestNavigationBusinessErrorStillExposed(t *testing.T) {
	_, _, navSvc, _ := newNavigationEnv(t)
	h := navigationhttp.NewHandle(navSvc)

	c, w := newNavErrContext(http.MethodGet, "/api/navigation/get?id=00000000-0000-0000-0000-000000000001", nil)
	h.Get(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在的导航项应为 404，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), navigationenums.ErrNotFound) {
		t.Errorf("业务错误应原样透出 %q，实际 %s", navigationenums.ErrNotFound, w.Body.String())
	}
	assertNoInternalLeak(t, w.Body.String())
}
