package builtin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 安全响应头完整性与 X-Powered-By 清理。
func TestSecurityHeadersMiddleware(t *testing.T) {
	engine := gin.New()
	engine.Use(SecurityHeadersMiddleware())
	engine.GET("/ok", func(c *gin.Context) {
		// 模拟上游（模板/第三方库）意外残留的技术栈头。
		c.Header("X-Powered-By", "go_wp/test")
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if got := recorder.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options 必须为 SAMEORIGIN（DENY 会破坏构建器同源 iframe）: got=%q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options 未生效: got=%q", got)
	}
	if got := recorder.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy 未生效: got=%q", got)
	}
	csp := recorder.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"frame-ancestors 'self'", "script-src 'self'"} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("CSP 缺少指令: directive=%s policy=%s", directive, csp)
		}
	}
	if got := recorder.Header().Get("X-Powered-By"); got != "" {
		t.Fatalf("X-Powered-By 应被防御性删除: got=%q", got)
	}
}
