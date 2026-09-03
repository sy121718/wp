package builtin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go_wp/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// TestCSRFMiddlewareRejectsMissingAndMismatchedToken 验证 CSRF 校验：
// 缺失 token、token 不匹配、无 cookie 均拒绝（403），正确 token 放行。
func TestCSRFMiddlewareRejectsMissingAndMismatchedToken(t *testing.T) {
	cfg := viper.New()
	cfg.Set("auth.session_secret", "csrf-test-secret")
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败：%v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	engine := gin.New()
	engine.POST("/write", CSRFMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 先通过一个 GET 生成 CSRF token，并拿到 session cookie。
	tokenEngine := gin.New()
	tokenEngine.GET("/token", func(c *gin.Context) {
		token, err := EnsureCSRFToken(c)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, token)
	})
	tokenRec := httptest.NewRecorder()
	tokenReq, _ := http.NewRequest(http.MethodGet, "/token", nil)
	tokenEngine.ServeHTTP(tokenRec, tokenReq)
	token := tokenRec.Body.String()

	var cookieHeader string
	for _, ck := range tokenRec.Result().Cookies() {
		if ck.Name == "gowp_session" {
			cookieHeader = ck.Name + "=" + ck.Value
		}
	}
	if token == "" || cookieHeader == "" {
		t.Fatalf("生成 CSRF token 或 cookie 失败")
	}

	post := func(headerToken string, withCookie bool) int {
		rec := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/write", nil)
		if withCookie {
			req.Header.Set("Cookie", cookieHeader)
		}
		if headerToken != "" {
			req.Header.Set("X-CSRF-Token", headerToken)
		}
		engine.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post("", true); code != http.StatusForbidden {
		t.Fatalf("缺失 CSRF token 应返回 403：got=%d", code)
	}
	if code := post("wrong-token", true); code != http.StatusForbidden {
		t.Fatalf("CSRF token 不匹配应返回 403：got=%d", code)
	}
	if code := post(token, true); code != http.StatusOK {
		t.Fatalf("正确 CSRF token 应放行：got=%d", code)
	}
	if code := post(token, false); code != http.StatusForbidden {
		t.Fatalf("无 cookie 时即使 token 正确也应 403：got=%d", code)
	}
}
