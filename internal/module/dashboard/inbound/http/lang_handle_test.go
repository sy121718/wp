package dashboardhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newLangSwitchEngine 只挂语言切换 handler（不挂 Session/CSRF，单测直测 handler 语义）。
func newLangSwitchEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)

	e := gin.New()
	e.GET("/admin/lang", LangSwitch)
	return e
}

// TestLangSwitch 覆盖 GET /admin/lang：写 Cookie + 302 回跳 + 开放重定向防护 + 非法输入不报错。
func TestLangSwitch(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		wantLang   string
		wantLoc    string
		wantStatus int
	}{
		{"合法 lang 写 Cookie 并回跳站内路径", "/admin/lang?lang=en-US&redirect=/admin/pages", "en-US", "/admin/pages", http.StatusFound},
		{"简码 en 规范化", "/admin/lang?lang=en", "en-US", "/", http.StatusFound},
		{"zh-Hans 规范化为 zh-CN", "/admin/lang?lang=zh-Hans", "zh-CN", "/", http.StatusFound},
		{"大写 EN-us 规范化", "/admin/lang?lang=EN-us", "en-US", "/", http.StatusFound},
		{"非法 lang 回退默认语言（不 4xx）", "/admin/lang?lang=ja-JP&redirect=/admin/pages", "zh-CN", "/admin/pages", http.StatusFound},
		{"超长 lang 回退默认语言", "/admin/lang?lang=aaaaaaaaaaaa", "zh-CN", "/", http.StatusFound},
		{"缺 lang 回退默认语言", "/admin/lang", "zh-CN", "/", http.StatusFound},
		{"空 lang 回退默认语言", "/admin/lang?lang=&redirect=/admin/media", "zh-CN", "/admin/media", http.StatusFound},
		{"开放重定向 //evil.com 回首页", "/admin/lang?lang=en-US&redirect=//evil.com", "en-US", "/", http.StatusFound},
		{"开放重定向 http:// 回首页", "/admin/lang?lang=en-US&redirect=http://evil.com/x", "en-US", "/", http.StatusFound},
		{"相对路径回首页", "/admin/lang?lang=en-US&redirect=admin/pages", "en-US", "/", http.StatusFound},
		{"站内路径含 // 回首页", "/admin/lang?lang=en-US&redirect=/a//b", "en-US", "/", http.StatusFound},
		{"站内路径带 query 保留", "/admin/lang?lang=en-US&redirect=/admin/pages?tab=draft", "en-US", "/admin/pages?tab=draft", http.StatusFound},
	}

	engine := newLangSwitchEngine()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.Header.Set("Accept-Language", "en-US,zh-CN;q=0.8")

			engine.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if got := w.Header().Get("Location"); got != tc.wantLoc {
				t.Fatalf("Location = %q, want %q", got, tc.wantLoc)
			}

			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie 数量 = %d, want 1（原始头：%q）", len(cookies), w.Header().Get("Set-Cookie"))
			}
			ck := cookies[0]
			if ck.Name != "lang" || ck.Value != tc.wantLang {
				t.Fatalf("Cookie = %s=%s, want lang=%s", ck.Name, ck.Value, tc.wantLang)
			}
			if ck.Path != "/" {
				t.Fatalf("Cookie Path = %q, want /", ck.Path)
			}
			if !ck.HttpOnly {
				t.Fatal("Cookie 缺少 HttpOnly")
			}
			if ck.SameSite != http.SameSiteLaxMode {
				t.Fatalf("Cookie SameSite = %v, want Lax", ck.SameSite)
			}
			if ck.MaxAge <= 0 {
				t.Fatalf("Cookie MaxAge = %d, want > 0", ck.MaxAge)
			}
		})
	}
}

// TestLangSwitchOpenRedirectGuard 直接覆盖回跳白名单函数。
func TestLangSwitchOpenRedirectGuard(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"/admin/pages", "/admin/pages"},
		{"/admin/pages?tab=draft", "/admin/pages?tab=draft"},
		{"  /admin/media  ", "/admin/media"},
		{"", "/"},
		{"admin/pages", "/"},
		{"//evil.com", "/"},
		{"http://evil.com", "/"},
		{"https://evil.com/a", "/"},
		{"/a//b", "/"},
		{"/\\evil.com", "/"},
		{"/admin\r\nX: 1", "/"},
		{"javascript:alert(1)", "/"},
	}

	for _, tc := range cases {
		if got := safeLangRedirect(tc.raw); got != tc.want {
			t.Fatalf("safeLangRedirect(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
