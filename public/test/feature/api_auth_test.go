package feature

// api_auth_test.go — 业务 API 统一认证（SessionAuthMiddleware）的接口链路测试。
//
// 覆盖点：
//   - GET /api/captcha 豁免认证，匿名可达
//   - 未登录访问业务 API（JSON 请求）→ 401 JSON
//   - 未登录的页面类请求（Accept: text/html 的 GET / HTMX）→ 302 登录页
//   - 登录成功（验证码 + PG + Redis 真实链路）后带 Cookie 访问业务 API → 200
//
// 依赖本地 PostgreSQL 与 Redis，任一不可用时整体 Skip（不 Fail）。

import (
	"net/http"
	"testing"

	"go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	"go_wp/pkg/response"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// newAuthFeatureEngine 组装带统一认证的业务 API 测试引擎，返回引擎与已登录管理员 Cookie。
// PG / Redis 任一不可用时 t.Skip；其余环节失败视为测试自身问题，直接 Fatal。
func newAuthFeatureEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用（%v）", err)
	}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			// 与 routes.go 相同的认证装配结构：captcha 与 admin 豁免，业务 API 统一认证。
			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminhttp.SetupAdminRoutes(api, db)

			authorizedAPI := api.Group("", builtin.SessionAuthMiddleware())
			authorizedAPI.GET("/ping", func(c *gin.Context) {
				response.Success(c, gin.H{"pong": true})
			})
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	if err := support.SetupRedisForTest(t); err != nil {
		cleanup()
		t.Skipf("跳过：%v", err)
	}

	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试管理员失败: %v", err)
	}

	cookie, err := support.LoginAdmin(t, engine, support.TestAdminUsername, support.TestAdminPassword)
	if err != nil {
		t.Skipf("跳过：登录链路不可用（%v）", err)
	}
	return engine, cookie
}

func TestCaptchaRouteExemptedFromAuth(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/captcha",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("验证码路由应豁免认证: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var data struct {
		CaptchaID string `json:"captcha_id"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if data.CaptchaID == "" {
		t.Fatalf("验证码路由应返回 captcha_id")
	}
}

func TestApiBusinessRoutesRejectAnonymousJSONRequest(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 JSON 请求应返回 401: got=%d want=%d", recorder.Code, http.StatusUnauthorized)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("响应 code 不正确: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}
}

func TestApiBusinessRoutesRedirectPageRequestsToLogin(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	// 浏览器页面导航：GET + Accept: text/html → 302 登录页。
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusFound {
		t.Fatalf("页面类 GET 请求应 302: got=%d want=%d", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/login" {
		t.Fatalf("重定向地址不正确: got=%s want=%s", got, "/admin/login")
	}

	// HTMX 请求：HX-Request: true → 302 登录页。
	recorder, err = support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"HX-Request": "true",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusFound {
		t.Fatalf("HTMX 请求应 302: got=%d want=%d", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/login" {
		t.Fatalf("HTMX 重定向地址不正确: got=%s want=%s", got, "/admin/login")
	}
}

func TestApiBusinessRoutesAllowLoggedInAdmin(t *testing.T) {
	engine, cookie := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Cookie": cookie,
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("已登录管理员应可访问业务 API: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var data struct {
		Pong bool `json:"pong"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if !data.Pong {
		t.Fatalf("handler 应正常执行并返回 pong=true")
	}
}
