package builtin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestSetupAttachesRecoverySecurityAndRateLimit(t *testing.T) {
	ResetRateLimitStore()

	engine := gin.New()
	// 直接测试 Setup 通过 handler 挂载中间件的能力
	engine.Use(Recovery())
	engine.Use(SecurityHeadersMiddleware())
	engine.Use(RequestBodyLimitMiddleware(10, 100))
	engine.Use(RequestRateLimitMiddleware(2, time.Minute))

	engine.GET("/ok", func(c *gin.Context) {
		response.Success(c, gin.H{"ok": true})
	})
	engine.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})
	engine.POST("/limited", func(c *gin.Context) {
		response.Success(c, gin.H{"ok": true})
	})

	okRecorder := httptest.NewRecorder()
	okRequest, _ := http.NewRequest(http.MethodGet, "/ok", nil)
	engine.ServeHTTP(okRecorder, okRequest)
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("正常请求状态码不正确: got=%d want=%d", okRecorder.Code, http.StatusOK)
	}
	if got := okRecorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("安全头未生效: got=%s want=%s", got, "nosniff")
	}
	contentSecurityPolicy := okRecorder.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
	} {
		if !strings.Contains(contentSecurityPolicy, directive) {
			t.Fatalf("CSP 缺少前端资源指令: directive=%s policy=%s", directive, contentSecurityPolicy)
		}
	}

	for i := 0; i < 2; i++ {
		rateRecorder := httptest.NewRecorder()
		rateRequest, _ := http.NewRequest(http.MethodGet, "/ok", nil)
		engine.ServeHTTP(rateRecorder, rateRequest)
	}
	blockedRecorder := httptest.NewRecorder()
	blockedRequest, _ := http.NewRequest(http.MethodGet, "/ok", nil)
	engine.ServeHTTP(blockedRecorder, blockedRequest)
	if blockedRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("第三次请求应被限流: got=%d want=%d", blockedRecorder.Code, http.StatusTooManyRequests)
	}

	bodyRecorder := httptest.NewRecorder()
	bodyRequest, _ := http.NewRequest(http.MethodPost, "/limited", strings.NewReader(strings.Repeat("a", 11)))
	bodyRequest.ContentLength = 11
	engine.ServeHTTP(bodyRecorder, bodyRequest)
	if bodyRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("请求体限制未生效: got=%d want=%d", bodyRecorder.Code, http.StatusRequestEntityTooLarge)
	}

	panicRecorder := httptest.NewRecorder()
	panicRequest, _ := http.NewRequest(http.MethodGet, "/panic", nil)
	engine.ServeHTTP(panicRecorder, panicRequest)
	if panicRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("Recovery 未生效: got=%d want=%d", panicRecorder.Code, http.StatusInternalServerError)
	}
}

func TestRequestRateLimitSkipsStaticAssets(t *testing.T) {
	ResetRateLimitStore()

	engine := gin.New()
	engine.Use(RequestRateLimitMiddleware(1, time.Minute))
	engine.GET("/static/*path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET("/storage/*path", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for _, path := range []string{
		"/static/builder/components/gallery/style.css",
		"/storage/uploads/example.png",
	} {
		for i := 0; i < 3; i++ {
			recorder := httptest.NewRecorder()
			request, _ := http.NewRequest(http.MethodGet, path, nil)
			engine.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("静态资源请求不应被限流: path=%s request=%d got=%d want=%d", path, i+1, recorder.Code, http.StatusOK)
			}
		}
	}
}
