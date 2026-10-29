package feature

import (
	"net/http"
	"testing"
)

// TestAccountProfileJSONNeverNavigates 不接受next驱动后端跳转；前端具名结果处理决定视图。
func TestAccountProfileJSONNeverNavigates(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "nextuser", "next@example.invalid", "password123")
	cookies := loginAs(t, env, "nextuser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)
	for _, next := range []string{"/my-account", "//evil.example", "https://evil.example/x", "evil.example"} {
		rec := env.postForm(t, "/api/site/user/account/profile", map[string]string{"csrf_token": token, "nickname": "保留本页", "next": next}, cookies)
		siteData(t, rec, http.StatusOK)
		if rec.Header().Get("Location") != "" {
			t.Fatalf("next=%q不应让JSON写入导航", next)
		}
	}
}

func TestRetiredFixedUserRoutesHaveNoHandlers(t *testing.T) {
	env := newUserHTTPEnv(t)
	for _, path := range []string{"/user/register", "/user/login", "/user/activate", "/user/forgot", "/user/reset", "/user/account"} {
		if rec := env.get(t, path, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("旧固定页面%s仍有处理器: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/user/register", "/user/login", "/user/resend", "/user/logout", "/user/forgot", "/user/reset", "/user/account/profile", "/user/account/preference", "/user/account/password", "/user/account/sessions/revoke", "/user/account/sessions/revoke-others"} {
		if rec := env.postForm(t, path, map[string]string{}, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("旧HTML写入口%s仍有处理器: %d", path, rec.Code)
		}
	}
}
