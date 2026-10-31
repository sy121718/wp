package builtin

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestRequestRateLimitBlocksOverLimit(t *testing.T) {
	engine := gin.New()
	engine.GET("/api/demo", RequestRateLimitMiddleware(2, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 1; i <= 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
		engine.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应放行: got=%d want=%d", i, recorder.Code, http.StatusOK)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次请求应被限流: got=%d want=%d", recorder.Code, http.StatusTooManyRequests)
	}
	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("限流响应应为统一 Response JSON: err=%v body=%s", err, recorder.Body.String())
	}
	if resp.Code != http.StatusTooManyRequests || resp.Message != "请求过于频繁" {
		t.Fatalf("限流响应内容不正确: code=%d message=%s", resp.Code, resp.Message)
	}
}

func TestRequestRateLimitIgnoresForwardedFor(t *testing.T) {
	engine := gin.New()
	engine.GET("/api/demo", RequestRateLimitMiddleware(1, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRecorder()
	engine.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/demo", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("首次请求应放行: got=%d want=%d", first.Code, http.StatusOK)
	}

	forged := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
	forged.Header.Set("X-Forwarded-For", "203.0.113.7")
	second := httptest.NewRecorder()
	engine.ServeHTTP(second, forged)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("伪造 X-Forwarded-For 不应绕过限流: got=%d want=%d", second.Code, http.StatusTooManyRequests)
	}
}

func TestRequestRateLimitInstancesAreIndependent(t *testing.T) {
	first := gin.New()
	first.GET("/a", RequestRateLimitMiddleware(1, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })
	second := gin.New()
	second.GET("/b", RequestRateLimitMiddleware(1, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	first.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/a", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("实例 A 首次请求应放行: got=%d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	first.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/a", nil))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("实例 A 第二次请求应被限流: got=%d want=%d", recorder.Code, http.StatusTooManyRequests)
	}

	recorder = httptest.NewRecorder()
	second.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/b", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("实例 B 与 A 计数应相互独立: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}

func TestRequestRateLimitSkipsInvalidParams(t *testing.T) {
	engine := gin.New()
	engine.GET("/api/demo", RequestRateLimitMiddleware(0, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 1; i <= 3; i++ {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/demo", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("非法参数下所有请求应放行: 第 %d 次 got=%d", i, recorder.Code)
		}
	}
}
