package feature

import (
	"net/http"
	"testing"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/response"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func TestLivezSuccess(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: true,
		InitComponents:  false,
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/livez",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码不正确: got=%d want=%d", recorder.Code, http.StatusOK)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if resp.Code != http.StatusOK {
		t.Fatalf("响应 code 不正确: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var data struct {
		Status string `json:"status"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}

	if data.Status != "alive" {
		t.Fatalf("响应 data.status 不正确: got=%s want=%s", data.Status, "alive")
	}
}

func TestReadyzReturnsServiceUnavailableWhenComponentsNotInitialized(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: true,
		InitComponents:  false,
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/readyz",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码不正确: got=%d want=%d", recorder.Code, http.StatusServiceUnavailable)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusServiceUnavailable)
	}

	var data struct {
		Status string `json:"status"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}

	if data.Status != "not_ready" {
		t.Fatalf("响应 data.status 不正确: got=%s want=%s", data.Status, "not_ready")
	}
}

func TestDefaultRoutesSetSecurityHeaders(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: true,
		InitComponents:  false,
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/livez",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options 不正确: got=%s want=%s", got, "nosniff")
	}
	if got := recorder.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options 不正确: got=%s want=%s", got, "SAMEORIGIN")
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatalf("Content-Security-Policy 不应为空")
	}
}

func TestJWTAuthMiddlewareAbortsOnMissingAuthorization(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.GET("/protected", builtin.JWTAuthMiddleware(), func(c *gin.Context) {
				response.Success(c, gin.H{"reached": true})
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

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/protected",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}

	var data struct {
		Reached bool `json:"reached"`
	}
	if err := support.DecodeResponseData(recorder, &data); err == nil {
		t.Fatalf("handler 不应继续执行，但返回了 data")
	}
}

func TestJWTAuthMiddlewareReturnsUnifiedUnauthorizedOnInvalidToken(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.GET("/protected", builtin.JWTAuthMiddleware(), func(c *gin.Context) {
				response.Success(c, gin.H{"reached": true})
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

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/protected",
		Headers: map[string]string{
			"Authorization": "Bearer invalid-token",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}

	var data struct {
		Reached bool `json:"reached"`
	}
	if err := support.DecodeResponseData(recorder, &data); err == nil {
		t.Fatalf("handler 不应继续执行，但返回了 data")
	}
}

func TestCasbinMiddlewareAbortsOnMissingUserContext(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.GET("/casbin-protected", builtin.CasbinMiddleware(), func(c *gin.Context) {
				response.Success(c, gin.H{"reached": true})
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

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/casbin-protected",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("错误码不正确: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}

	var data struct {
		Reached bool `json:"reached"`
	}
	if err := support.DecodeResponseData(recorder, &data); err == nil {
		t.Fatalf("handler 不应继续执行，但返回了 data")
	}
}
