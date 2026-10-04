package runtimefragment

// 片段端点与能力白名单测试（0-D，docs/04 §1.2）：白名单拒绝未知能力、
// handler escape 用户数据、认证策略、参数限制。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestFragmentEndpointLangHeader 语言解析结果写进响应头；非法 lang 回落默认（I18N-011）。
//
// 这条用例同时钉住两件事：
//
//  1. Content-Language 必须是**真正生效**的那个语言（清单里没有的值要回落，
//     而不是原样回声 —— 回声会让「语言没生效」看起来像生效了）；
//  2. 响应**不得**带 `Vary: Accept-Language`。片段语言只由 URL 的 ?lang 决定
//     （见 resolveRequestLang），声明一个不参与内容选择的请求头会让 CDN 为同一份
//     字节建多个缓存桶（写错 Vary 比不写更糟）。
func TestFragmentEndpointLangHeader(t *testing.T) {
	deps.FragmentProject = &stubProjectLocales{langs: []string{"zh-CN", "en-US"}, def: "zh-CN"}
	defer func() { deps.FragmentProject = nil }()
	r := newRouter()

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"启用的语言", "?projectId=p1&lang=en-US", "en-US"},
		{"未启用的 lang 回落工程默认", "?projectId=p1&lang=not-a-real-lang", "zh-CN"},
		{"不带 lang 取工程默认", "?projectId=p1", "zh-CN"},
	}
	for _, tc := range cases {
		w := doGet(t, r, "/_fragments/loginPanel"+tc.query)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: 应 200，实际 %d", tc.name, w.Code)
		}
		if got := w.Header().Get("Content-Language"); got != tc.want {
			t.Errorf("%s: Content-Language 期望 %q 实际 %q", tc.name, tc.want, got)
		}
		if got := w.Header().Get("Vary"); strings.Contains(got, "Accept-Language") {
			t.Errorf("%s: 语言不由 Accept-Language 决定，不该在 Vary 里声明（实际 %q）", tc.name, got)
		}
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

// TestFragmentEndpointProductListParamBudget 商品列表片段的典型请求必须过得去（审计 PERF-019）。
//
// 实例配置本身就有十几个参数名（实测典型配置 13 个），再加筛选、属性维度与页码。
// 上限只有 10 时这些请求被自己拒成 400（「参数过多」），而且只在真实 HTTP 路径上发生 ——
// 单测直调处理器看不见，正是它一直没被发现的原因 —— 所以这条走完整端点。
func TestFragmentEndpointProductListParamBudget(t *testing.T) {
	deps.CollectionResolver = &stubCollection{items: []map[string]any{listItem("shirt", "衬衫")}}
	defer func() { deps.CollectionResolver = nil }()
	params := url.Values{}
	for k, v := range map[string]string{
		// 实例配置：由产物烘入（productlist 侧 fragmentQuery 的白名单）。
		"nodeId": "list-1", "projectId": "proj-1", "layout": "grid", "columns": "auto",
		"currency": "¥", "titleTag": "h3", "emptyText": "暂无商品", "linkPrefix": "/products/",
		"titleField": "item.name", "priceField": "item.priceRange", "linkField": "item.slug",
		"limit": "12", "pageSize": "12",
		// 语义参数：访客可改。
		"status": "published", "categoryId": "11111111-1111-1111-1111-111111111111",
		"option.color": "red", "option.size": "m", "page": "2",
	} {
		params.Set(k, v)
	}
	if len(params) <= 10 {
		t.Fatalf("这条用例要覆盖「参数名超过旧上限 10」的场景，当前只有 %d 个", len(params))
	}
	w := doGet(t, newRouter(), "/_fragments/productList?"+params.Encode())
	if w.Code != http.StatusOK {
		t.Fatalf("%d 个参数名的典型商品列表请求应 200，实际 %d: %s", len(params), w.Code, w.Body.String())
	}
}
