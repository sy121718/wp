package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/logger"
	"go_wp/pkg/safepath"
)

type devLoginAdminStub struct {
	admincontract.AdminService
	sessionID string
}

func (s devLoginAdminStub) DevLogin(context.Context, string) (*admindto.AdminLoginResp, error) {
	return &admindto.AdminLoginResp{UserID: 1, Username: "tester", SessionID: s.sessionID}, nil
}

func lastPathChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteString("\\x")
			const hex = "0123456789abcdef"
			b.WriteByte(hex[byte(r)>>4])
			b.WriteByte(hex[byte(r)&0x0f])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestSafeRedirect(t *testing.T) {
	long := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes)

	cases := []struct {
		name string
		in   string
		want string
		why  string
	}{

		{"根路径", "/", "/", "站内"},
		{"普通后台路径", "/admin/pages", "/admin/pages", "站内"},
		{"带筛选 query", "/admin/coupons?status=1&page=2", "/admin/coupons?status=1&page=2", "站内"},
		{"带片段", "/admin/pages#form", "/admin/pages#form", "站内"},

		{"TAB 夹在双斜杠之间", "/\t/evil.example.com", "/admin", "控制字符"},
		{"LF 夹在双斜杠之间", "/\n/evil.example.com", "/admin", "控制字符"},
		{"TAB + 反斜杠", "/\t\\evil.example.com", "/admin", "控制字符"},
		{"CR 夹在双斜杠之间", "/\r/evil.example.com", "/admin", "控制字符"},

		{"垂直制表", "/\x0b/admin", "/admin", "控制字符"},
		{"中段 NUL", "/admin\x00/pages", "/admin", "控制字符"},
		{"DEL", "/admin\x7fpages", "/admin", "控制字符"},

		{"CRLF 注入尝试", "/a\r\nLocation: https://evil.example.com", "/admin", "控制字符"},

		{"协议相对前缀", "//evil.example.com/x", "/admin", "前缀//"},
		{"前缀反斜杠（UNC 形态）", `/\\evil.example.com`, "/admin", "前缀/\\"},
		{"无前导斜杠", "admin/roles", "/admin", "非站内"},
		{"绝对 URL", "https://evil.example.com/x", "/admin", "非站内"},
		{"空串", "", "/admin", "非站内"},
		{"仅空白", "   ", "/admin", "非站内"},
		{"前后空白被 trim", " /admin ", "/admin", "TrimSpace 后合法"},

		{"内部双斜杠仍放行", "/a//b", "/a//b", "兼容：子串//不在本次范围"},
		{"query 里带双斜杠仍放行", "/admin?next=//evil.com", "/admin?next=//evil.com", "兼容：同上"},
		{"513 字节仍放行", long, long, "兼容：长度上限不在本次范围"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := safeRedirect(tc.in)
			if got != tc.want {
				t.Errorf("safeRedirect(%q) = %q，期望 %q（%s）", lastPathChars(tc.in), lastPathChars(got), lastPathChars(tc.want), tc.why)
			}

			if safepath.HasControlRune(got) {
				t.Errorf("出口 %q 仍含控制字符（%s）", lastPathChars(got), tc.why)
			}
			if got != "/admin" && !strings.HasPrefix(got, "/") {
				t.Errorf("出口 %q 不是站内绝对路径", lastPathChars(got))
			}
		})
	}
}

func withDevLoginInfra(t *testing.T) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisServer.Addr()})
	cfg.Set("auth.session_secret", "dev-login-safe-redirect-test-secret")
	cfg.Set("log.base_dir", t.TempDir())
	cfg.Set("log.level", "error")

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化测试缓存失败：%v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	if err := logger.Init(cfg); err != nil {
		t.Fatalf("初始化 logger 失败：%v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化测试会话存储失败：%v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })
}

func TestDevLoginRedirectLocationIsControlFree(t *testing.T) {
	withDevLoginInfra(t)
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handle := &Handle{admin: devLoginAdminStub{sessionID: "dev-login-test-session"}}
	engine.GET("/admin/dev-login", handle.DevLogin)

	cases := []struct {
		name  string
		to    string
		want  string
		check func(t *testing.T, location string)
	}{
		{"普通站内路径", "/admin/pages", "/admin/pages", nil},
		{"带 query 的站内路径", "/admin/coupons?status=1", "/admin/coupons?status=1", nil},
		{"TAB 夹在双斜杠之间", "/\t/evil.example.com", "/admin", nil},
		{"LF 夹在双斜杠之间", "/\n/evil.example.com", "/admin", nil},
		{"TAB + 反斜杠", "/\t\\evil.example.com", "/admin", nil},
		{"CRLF 注入尝试", "/a\r\nLocation: https://evil.example.com", "/admin", nil},

		{"内部双斜杠（既有兼容）", "/a//b", "/a/b", nil},
		{"513 字节（既有兼容）", "/" + strings.Repeat("a", safepath.MaxReturnPathBytes), "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/dev-login?to="+url.QueryEscape(tc.to), nil)
			req.RemoteAddr = "127.0.0.1:23456"
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("期望 302，实得 %d（body=%q）", rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			want := tc.want
			if want == "" {
				want = "/" + strings.Repeat("a", safepath.MaxReturnPathBytes)
			}
			if loc != want {
				t.Errorf("Location = %q，期望 %q", lastPathChars(loc), lastPathChars(want))
			}
			if safepath.HasControlRune(loc) {
				t.Errorf("Location 仍含控制字符：%q", lastPathChars(loc))
			}
			if !strings.HasPrefix(loc, "/") {
				t.Errorf("Location 不是站内绝对路径：%q", lastPathChars(loc))
			}
		})
	}
}

func TestDevLoginRejectsNonLoopback(t *testing.T) {
	withDevLoginInfra(t)
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handle := &Handle{admin: devLoginAdminStub{sessionID: "dev-login-test-session"}}
	engine.GET("/admin/dev-login", handle.DevLogin)

	req := httptest.NewRequest(http.MethodGet, "/admin/dev-login?to=/admin", nil)
	req.RemoteAddr = "203.0.113.7:51234"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("非环回请求期望 403，实得 %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("非环回请求不应带 Location，实得 %q", loc)
	}
}
