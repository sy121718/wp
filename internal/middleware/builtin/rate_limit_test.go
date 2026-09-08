package builtin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 同一 IP 超过窗口额度后返回 429，响应体为统一 Response JSON 结构。
func TestRequestRateLimitBlocksOverLimit(t *testing.T) {
	engine := gin.New()
	engine.GET("/api/demo", RequestRateLimitMiddleware(2, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 前两次正常放行（窗口额度内）。
	for i := 1; i <= 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
		engine.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求应放行: got=%d want=%d", i, recorder.Code, http.StatusOK)
		}
	}

	// 第三次超限：429 + 统一 Response JSON + 中文提示。
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

// 伪造 X-Forwarded-For 不影响限流计数：key 仅取 RemoteAddr，
// 与 release 模式 TrustedProxies=nil 的 ClientIP 语义一致。
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

	// 换一个伪造的 XFF 仍视为同一客户端，第二次请求应被限流。
	forged := httptest.NewRequest(http.MethodGet, "/api/demo", nil)
	forged.Header.Set("X-Forwarded-For", "203.0.113.7")
	second := httptest.NewRecorder()
	engine.ServeHTTP(second, forged)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("伪造 X-Forwarded-For 不应绕过限流: got=%d want=%d", second.Code, http.StatusTooManyRequests)
	}
}

// 每次调用工厂函数创建独立限流器实例：实例之间计数互不影响。
func TestRequestRateLimitInstancesAreIndependent(t *testing.T) {
	first := gin.New()
	first.GET("/a", RequestRateLimitMiddleware(1, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })
	second := gin.New()
	second.GET("/b", RequestRateLimitMiddleware(1, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })

	// 实例 A 额度耗尽。
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

	// 实例 B 额度独立，首次请求不受 A 影响。
	recorder = httptest.NewRecorder()
	second.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/b", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("实例 B 与 A 计数应相互独立: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}

// 非法参数（limit/window 非正数）直接放行，不构造限流器。
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
