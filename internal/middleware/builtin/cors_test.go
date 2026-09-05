package builtin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCORSRequest(method, path, origin string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func serveCORS(middleware gin.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.Use(middleware)
	engine.GET("/data", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	engine.POST("/data", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

// 白名单模式：命中白名单的 Origin 回显并允许携带 Cookie。
func TestCORSAllowlistMatchesOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := CORS(CORSConfig{AllowedOrigins: []string{"https://admin.example.com"}})

	recorder := serveCORS(middleware, newCORSRequest(http.MethodGet, "/data", "https://admin.example.com"))
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("命中白名单应回显 Origin: got=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("白名单模式应保留 Allow-Credentials: got=%q", got)
	}
}

// 白名单模式：未命中的 Origin 不反射、不下发任何 CORS 头（与 credentials 永不冲突）。
func TestCORSAllowlistRejectsOtherOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := CORS(CORSConfig{AllowedOrigins: []string{"https://admin.example.com"}})

	recorder := serveCORS(middleware, newCORSRequest(http.MethodGet, "/data", "https://evil.example.com"))
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("未命中白名单不应下发 ACAO（不反射）: got=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("未命中白名单不应下发 Allow-Credentials: got=%q", got)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("非预检请求仍应执行业务: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}

// 预检：命中返回 204 + CORS 头；未命中返回 204 但不附加 CORS 头（浏览器侧拒绝）。
func TestCORSPreflightWithAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := CORS(CORSConfig{AllowedOrigins: []string{"https://admin.example.com"}})

	recorder := serveCORS(middleware, newCORSRequest(http.MethodOptions, "/data", "https://admin.example.com"))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("预检应返回 204: got=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("预检命中白名单应下发 ACAO: got=%q", got)
	}

	rejected := serveCORS(middleware, newCORSRequest(http.MethodOptions, "/data", "https://evil.example.com"))
	if rejected.Code != http.StatusNoContent {
		t.Fatalf("未命中白名单的预检也应返回 204: got=%d", rejected.Code)
	}
	if got := rejected.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("未命中白名单的预检不应下发 ACAO: got=%q", got)
	}
}

// 无白名单 + release：拒绝跨域（不反射 Origin），业务仍执行。
func TestCORSReleaseRefusesWithoutAllowlist(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(gin.TestMode)

	middleware := CORS() // 无白名单（测试环境 config 未初始化）

	recorder := serveCORS(middleware, newCORSRequest(http.MethodGet, "/data", "https://evil.example.com"))
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("release 无白名单不应反射 Origin: got=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("release 无白名单不应下发 Allow-Credentials: got=%q", got)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("非预检请求仍应执行业务: got=%d want=%d", recorder.Code, http.StatusOK)
	}
}

// 无白名单 + debug：回退反射 Origin 并允许 credentials（仅限开发环境）。
func TestCORDebugReflectsWithoutAllowlist(t *testing.T) {
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(gin.TestMode)

	middleware := CORS() // 无白名单 → debug 回退反射

	recorder := serveCORS(middleware, newCORSRequest(http.MethodGet, "/data", "https://dev.example.com"))
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://dev.example.com" {
		t.Fatalf("debug 无白名单应反射 Origin: got=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("debug 反射模式应允许 credentials: got=%q", got)
	}
}

// 无 Origin（同源/curl）请求直接放行，不附加 CORS 头。
func TestCORSNoOriginPassesThrough(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(gin.TestMode)

	middleware := CORS()

	recorder := serveCORS(middleware, newCORSRequest(http.MethodGet, "/data", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("无 Origin 请求应直接放行: got=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("无 Origin 不应附加 CORS 头: got=%q", got)
	}
}

func TestOriginAllowed(t *testing.T) {
	allowlist := []string{"https://a.example.com", " https://b.example.com "}
	cases := []struct {
		origin string
		want   bool
	}{
		{"https://a.example.com", true},
		{"https://A.EXAMPLE.COM", true}, // 大小写不敏感
		{"https://b.example.com", true},
		{"https://evil.example.com", false},
		{"https://a.example.com.evil.com", false}, // 前缀/子串不匹配
	}
	for _, tc := range cases {
		if got := originAllowed(tc.origin, allowlist); got != tc.want {
			t.Fatalf("originAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
	if !originAllowed("https://any.example.com", []string{"*"}) {
		t.Fatalf("白名单含 * 应允许任意来源")
	}
}

func TestNormalizeOrigins(t *testing.T) {
	got := normalizeOrigins([]string{" https://a.example.com ", "", "https://a.example.com", "https://b.example.com"})
	want := []string{"https://a.example.com", "https://b.example.com"}
	if len(got) != len(want) {
		t.Fatalf("normalizeOrigins 长度错误: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeOrigins 内容错误: got=%v want=%v", got, want)
		}
	}
	if got := strings.Join(normalizeOrigins(nil), ","); got != "" {
		t.Fatalf("nil 白名单应归一为空: got=%q", got)
	}
}
