package feature

// navigation_error_leak_test.go — 导航 JSON 接口不直出内部错误（审计 CQ-009，迁移 269 的验收）。
//
// 此前 5 个接口都是 `response.ErrorWithMessage(c, navigationErrorStatus(err), err.Error())`，
// 而 navigationErrorStatus 的 default 分支是 500 —— service 上抛的 PostgreSQL 原文会随 500
// 原样直出（表名、唯一约束名、SQLSTATE）。现在统一走 navigation_err.go 的归口助手，
// 本文件是那条改造的回归门禁。
//
// 断言是**强**的：响应体里出现 SQLSTATE / uq_ / pg_ / uuid / 表名 / 约束名任一即失败。
// 放宽成「只要出错就行」等于把这条回归重新交还给运气。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationenums "go_wp/internal/module/navigation/enums"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
)

// internalLeakTokens 内部细节的指纹：出现任一即视为泄漏。
//
// 取的是「PG 原文与库结构里一定出现、而业务文案里一定不出现」的串：
// uuid 语法错误的原文形如
// `ERROR: invalid input syntax for type uuid: "not-a-uuid" (SQLSTATE 22P02)`。
var internalLeakTokens = []string{
	"SQLSTATE", "uq_", "pg_", "navigations", "constraint", "uuid", "invalid input syntax",
}

// assertNoInternalLeak 断言响应体不含任何内部细节指纹。
func assertNoInternalLeak(t *testing.T, body string) {
	t.Helper()
	for _, tok := range internalLeakTokens {
		if strings.Contains(body, tok) {
			t.Errorf("响应体泄漏内部细节 %q: %s", tok, body)
		}
	}
}

// newNavErrContext 构造不经过三层鉴权链的 gin 上下文（handler 级测试：只验响应体）。
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

// TestNavigationListHidesInternalError List 收到非法工程 id：500 + 归口文案，无 PG 原文。
//
// project_id 是 uuid 列（迁移 046），非 uuid 会让 PG 报 22P02 —— 这正是
// 「service 把数据库原文上抛」的真实路径，不是人为构造的错误字符串。
func TestNavigationListHidesInternalError(t *testing.T) {
	_, _, navSvc, _ := newNavigationEnv(t)
	h := navigationhttp.NewHandle(navSvc)

	c, w := newNavErrContext(http.MethodGet, "/api/navigation/list?projectId=not-a-uuid&kind=header", nil)
	h.List(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非法工程 id 属内部错误，状态码应为 500，实际 %d，body=%s", w.Code, w.Body.String())
	}
	assertNoInternalLeak(t, w.Body.String())
	if !strings.Contains(w.Body.String(), navigationenums.ErrInternal) {
		t.Errorf("响应体应返回归口文案 %q，实际 %s", navigationenums.ErrInternal, w.Body.String())
	}
}

// TestNavigationCreateHidesInternalError Create 同样不直出数据库原文。
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

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非法工程 id 属内部错误，状态码应为 500，实际 %d，body=%s", w.Code, w.Body.String())
	}
	assertNoInternalLeak(t, w.Body.String())
}

// TestNavigationBusinessErrorStillExposed 业务错误原样透出：
// 归口助手不是「一律吞成通用文案」，否则前端再也无法提示「哪一项不合法」。
func TestNavigationBusinessErrorStillExposed(t *testing.T) {
	_, _, navSvc, _ := newNavigationEnv(t)
	h := navigationhttp.NewHandle(navSvc)

	// 合法 uuid 但不存在 → 命中白名单 ErrNotFound（404 语义不变）。
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
