package runtimefragment

// 片段端点与能力白名单测试（0-D，docs/04 §1.2）：白名单拒绝未知能力、
// handler escape 用户数据、认证策略、参数限制。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newRouter 挂载片段端点路由的测试引擎。
func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupFragmentRoutes(r)
	return r
}

func doGet(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestFragmentEndpointKnownCapability 已注册能力返回 HTML 片段。
func TestFragmentEndpointKnownCapability(t *testing.T) {
	r := newRouter()
	w := doGet(t, r, "/_fragments/loginPanel")
	if w.Code != http.StatusOK {
		t.Fatalf("已知能力应 200: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "login-panel") {
		t.Fatalf("应返回登录面板片段: %s", w.Body.String())
	}
}

// TestFragmentEndpointUnknownCapability 未知能力白名单拒绝（不接受任意 endpoint）。
func TestFragmentEndpointUnknownCapability(t *testing.T) {
	r := newRouter()
	for _, path := range []string{"/_fragments/evil", "/_fragments/../../etc/passwd", "/_fragments/admin/users"} {
		w := doGet(t, r, path)
		if w.Code != http.StatusNotFound {
			t.Fatalf("未知能力 %q 应 404: %d", path, w.Code)
		}
	}
}

// TestFragmentEndpointSessionAuth session 能力未登录拒绝。
//
// 这里注册一个**测试专用**的 session 能力，而不是拿某个业务能力当样本：
// 业务能力的认证策略会随产品需要变化（购物车就是从 session 改成 anonymous 的 ——
// 访客必须能在登录之前加购）。拿业务能力当样本，会让一条与它无关的协议断言
// 跟着一起变红，而红的原因还看不出是协议坏了还是产品改了。
func TestFragmentEndpointSessionAuth(t *testing.T) {
	const probe = "sessionProbeTestOnly"
	Register(Spec{
		Type: probe, Method: "GET", Auth: AuthSession,
		Render: func(_ context.Context, _ *Request) (string, error) { return "<span>ok</span>", nil },
	})
	r := newRouter()
	w := doGet(t, r, "/_fragments/"+probe)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("session 能力未登录应 401: %d", w.Code)
	}
}

// TestFragmentEndpointParamLimit 参数超限/非法上下文拒绝。
func TestFragmentEndpointParamLimit(t *testing.T) {
	r := newRouter()
	// 非法上下文。
	if w := doGet(t, r, "/_fragments/loginPanel?context=evil"); w.Code != http.StatusBadRequest {
		t.Fatalf("非法上下文应 400: %d", w.Code)
	}
	// 超长参数。
	long := strings.Repeat("x", maxParamLen+1)
	if w := doGet(t, r, "/_fragments/loginPanel?q="+long); w.Code != http.StatusBadRequest {
		t.Fatalf("超长参数应 400: %d", w.Code)
	}
}

// TestFragmentRenderEscapes 处理器输出用户数据经 escape（loginPanel 的 label）。
func TestFragmentRenderEscapes(t *testing.T) {
	r := newRouter()
	w := doGet(t, r, "/_fragments/loginPanel?context=visitorSession")
	body := w.Body.String()
	// 输出是受控固定模板，不拼接未转义的用户输入（此处验证无注入面）。
	if strings.Contains(body, "<script") {
		t.Fatalf("片段不应含 script: %s", body)
	}
}

// TestRegistryTypes 能力白名单类型列表确定性。
func TestRegistryTypes(t *testing.T) {
	types := Types()
	if len(types) < 2 {
		t.Fatalf("内置能力应至少 2 个: %v", types)
	}
	// 确定性：两次调用一致。
	if a, b := Types(), Types(); strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("Types 不确定")
	}
}
