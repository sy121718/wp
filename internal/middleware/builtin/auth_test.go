package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/response"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func TestJWTSessionBecomesInvalidAfterLogout(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisServer.Addr()})
	cfg.Set("jwt.secret", "logout-session-test-secret")
	cfg.Set("jwt.expire_time", 24)

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化测试缓存失败：%v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化测试 JWT 失败：%v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	token, sessionID, err := auth.GenerateSessionToken(7, "tester", false, "")
	if err != nil {
		t.Fatalf("生成测试 token 失败：%v", err)
	}
	ctx := context.Background()
	if err = auth.SaveUserSession(ctx, &auth.UserSession{
		ID:        7,
		SessionID: sessionID,
		Username:  "tester",
	}, time.Hour); err != nil {
		t.Fatalf("保存测试会话失败：%v", err)
	}

	engine := gin.New()
	engine.GET("/protected", JWTAuthMiddleware(), func(c *gin.Context) {
		response.Success(c, nil)
	})

	request := func() int {
		recorder := httptest.NewRecorder()
		req, requestErr := http.NewRequest(http.MethodGet, "/protected", nil)
		if requestErr != nil {
			t.Fatalf("创建测试请求失败：%v", requestErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		engine.ServeHTTP(recorder, req)
		return recorder.Code
	}

	if status := request(); status != http.StatusOK {
		t.Fatalf("有效会话应允许访问：got=%d want=%d", status, http.StatusOK)
	}
	if err = auth.DeleteUserSession(ctx, 7); err != nil {
		t.Fatalf("注销测试会话失败：%v", err)
	}
	if status := request(); status != http.StatusUnauthorized {
		t.Fatalf("注销后的旧 token 应被拒绝：got=%d want=%d", status, http.StatusUnauthorized)
	}
}
