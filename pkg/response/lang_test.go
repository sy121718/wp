package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newLangCtx 构造带语言协商三要素的测试上下文（空串表示该层缺失）。
func newLangCtx(cookie, query, acceptLang string) *gin.Context {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if acceptLang != "" {
		req.Header.Set("Accept-Language", acceptLang)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: LangCookieName, Value: cookie})
	}
	if query != "" {
		q := req.URL.Query()
		q.Set("lang", query)
		req.URL.RawQuery = q.Encode()
	}

	c, _ := gin.CreateTestContext(nil)
	c.Request = req
	return c
}

// TestRequestLanguageCookiePriority 覆盖多语言 P1 的协商优先级：
// Cookie lang > query lang > Accept-Language 首段 > 配置默认语言。
func TestRequestLanguageCookiePriority(t *testing.T) {
	cases := []struct {
		name   string
		cookie string
		query  string
		accept string
		want   string
	}{
		{"Cookie 优先于 query 与 Accept-Language", "en-US", "zh-CN", "zh-CN", "en-US"},
		{"Cookie 优先于 query（中文 Cookie）", "zh-CN", "en-US", "en-US", "zh-CN"},
		{"Cookie 简码规范化 en→en-US", "en", "", "zh-CN", "en-US"},
		{"Cookie zh-Hans 规范化", "zh-Hans", "", "en-US", "zh-CN"},
		{"Cookie 大小写规范化 EN-us", "EN-us", "", "", "en-US"},
		{"非法 Cookie 回退默认语言（不报错）", "ja-JP", "", "en-US", "zh-CN"},
		{"超长 Cookie 回退默认语言", strings.Repeat("a", 11), "", "", "zh-CN"},
		{"无 Cookie 时 query 优先于 Accept-Language", "", "en-US", "zh-CN", "en-US"},
		{"无 Cookie/query 时取 Accept-Language", "", "", "en-US", "en-US"},
		{"三层全缺回退默认语言", "", "", "", "zh-CN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newLangCtx(tc.cookie, tc.query, tc.accept)
			if got := requestLanguage(c); got != tc.want {
				t.Fatalf("requestLanguage(cookie=%q, query=%q, accept=%q) = %q, want %q",
					tc.cookie, tc.query, tc.accept, got, tc.want)
			}
		})
	}
}

// TestRequestLanguageEmptyCookieFallsThrough 空 Cookie 值视为缺失，继续降级到 query。
func TestRequestLanguageEmptyCookieFallsThrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?lang=en-US", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	req.AddCookie(&http.Cookie{Name: LangCookieName, Value: ""})

	c, _ := gin.CreateTestContext(nil)
	c.Request = req

	if got := requestLanguage(c); got != "en-US" {
		t.Fatalf("空 Cookie 应降级到 query，got %q", got)
	}
}

// TestNormalizeLangExported 验证语言校验出口：合法返回规范化码 + true，非法返回默认语言 + false。
func TestNormalizeLangExported(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{"zh", "zh-CN", true},
		{"zh-Hans", "zh-CN", true},
		{"en", "en-US", true},
		{"en-GB", "en-US", true},
		{"en-US", "en-US", true},
		{"ja-JP", "zh-CN", false},
		{"", "zh-CN", false},
		{strings.Repeat("x", 11), "zh-CN", false},
	}

	for _, tc := range cases {
		got, ok := NormalizeLang(tc.raw)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("NormalizeLang(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestTranslateUnknownKeyFallback 兜底验证：库中不存在的 key 原样返回（三种形态均不报错、不 panic），
// 且与语言（含 Cookie 指定的语言）无关 —— 不依赖数据库，未加载缓存时同样成立。
func TestTranslateUnknownKeyFallback(t *testing.T) {
	cases := []struct {
		name    string
		cookie  string
		message string
		want    string
	}{
		{"未知 key 原样返回（默认语言）", "", "ErrNotExistInDB", "ErrNotExistInDB"},
		{"未知 key 原样返回（Cookie en-US）", "en-US", "ErrNotExistInDB", "ErrNotExistInDB"},
		{"未知 key|param 原样返回", "en-US", "ErrNotExistInDB|5m0s", "ErrNotExistInDB|5m0s"},
		{"未知 key: detail 原样返回", "en-US", "ErrNotExistInDB: 细节", "ErrNotExistInDB: 细节"},
		{"普通中文消息原样返回", "", "请求的资源不存在", "请求的资源不存在"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newLangCtx(tc.cookie, "", "")
			if got := translate(c, tc.message); got != tc.want {
				t.Fatalf("translate(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}

// TestSetLangCookieAttributes 验证 Cookie 属性：Path=/、HttpOnly、SameSite=Lax、MaxAge 一年。
func TestSetLangCookieAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	SetLangCookie(c, "en-US")

	raw := w.Header().Get("Set-Cookie")
	if raw == "" {
		t.Fatal("未写入 Set-Cookie")
	}
	for _, want := range []string{"lang=en-US", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age=31536000"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("Set-Cookie = %q, 缺少 %q", raw, want)
		}
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Cookie 数量 = %d, want 1", len(cookies))
	}
	if cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookies[0].SameSite)
	}
}

// TestSetLangCookieNilContext nil 上下文不应 panic。
func TestSetLangCookieNilContext(t *testing.T) {
	SetLangCookie(nil, "zh-CN")
}
