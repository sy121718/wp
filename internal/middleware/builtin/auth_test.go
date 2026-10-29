package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/enums"
	"go_wp/pkg/response"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// TestSessionBecomesInvalidAfterLogout 验证：登录写 cookie session 后可访问受保护路由，
// 登出（删除 Redis 会话）后旧 cookie 应被拒绝（401）。
func TestSessionBecomesInvalidAfterLogout(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisServer.Addr()})
	cfg.Set("auth.session_secret", "logout-session-test-secret")

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化测试缓存失败：%v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化测试会话存储失败：%v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	sessionID, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("生成测试会话 ID 失败：%v", err)
	}
	ctx := context.Background()
	if err = auth.SaveUserSession(ctx, &auth.UserSession{
		ID:        7,
		SessionID: sessionID,
		Username:  "tester",
	}, time.Hour); err != nil {
		t.Fatalf("保存测试会话失败：%v", err)
	}

	// 通过一个“登录”请求把 cookie session 写出来（模拟登录响应 Set-Cookie）。
	engine := gin.New()
	engine.GET("/login", func(c *gin.Context) {
		_ = auth.SaveCookieSession(c, &auth.CookieSession{
			SessionID: sessionID,
		}, false)
		c.Status(http.StatusOK)
	})
	loginRec := httptest.NewRecorder()
	loginReq, _ := http.NewRequest(http.MethodGet, "/login", nil)
	engine.ServeHTTP(loginRec, loginReq)

	var cookieHeader string
	for _, ck := range loginRec.Result().Cookies() {
		if ck.Name == "gowp_session" {
			cookieHeader = ck.Name + "=" + ck.Value
		}
	}
	if cookieHeader == "" {
		t.Fatalf("登录应写入 gowp_session cookie，但 Set-Cookie 缺失")
	}

	protected := gin.New()
	protected.GET("/protected", SessionAuthMiddleware(), func(c *gin.Context) {
		response.Success(c, nil)
	})

	request := func() int {
		recorder := httptest.NewRecorder()
		req, requestErr := http.NewRequest(http.MethodGet, "/protected", nil)
		if requestErr != nil {
			t.Fatalf("创建测试请求失败：%v", requestErr)
		}
		req.Header.Set("Cookie", cookieHeader)
		protected.ServeHTTP(recorder, req)
		return recorder.Code
	}

	if status := request(); status != http.StatusOK {
		t.Fatalf("有效会话应允许访问：got=%d want=%d", status, http.StatusOK)
	}
	if err = auth.DeleteUserSession(ctx, 7); err != nil {
		t.Fatalf("注销测试会话失败：%v", err)
	}
	if status := request(); status != http.StatusUnauthorized {
		t.Fatalf("注销后的旧 cookie 应被拒绝：got=%d want=%d", status, http.StatusUnauthorized)
	}
}

func TestSessionAuthRejectsPagesAndHTMXWithoutNavigation(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, hx := range []string{"", "true", "TRUE", " true ", "false"} {
			engine := gin.New()
			reached := false
			engine.Any("/protected", SessionAuthMiddleware(), func(c *gin.Context) { reached = true; c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(method, "/protected", nil)
			req.Header.Set("Accept", "text/html")
			req.Header.Set("HX-Request", hx)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if reached || rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "未登录或登录已过期") {
				t.Fatalf("%s HX=%q应拒绝为401：reached=%v status=%d body=%s", method, hx, reached, rec.Code, rec.Body.String())
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("认证拒绝应为不可缓存JSON：%v", rec.Header())
			}
			for _, header := range []string{"Location", "HX-Redirect", "HX-Location"} {
				if rec.Header().Get(header) != "" {
					t.Fatalf("认证拒绝不得输出导航头%s", header)
				}
			}
		}
	}
}

func TestAuthenticationStoreUnavailableKeeps503JSON(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/protected", nil)
	respondAuthFailure(c, enums.ErrAuthUnavailable)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "认证状态暂时不可用") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("认证依赖故障应为503 JSON：%d %s", rec.Code, rec.Body.String())
	}
}
