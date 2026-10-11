package adminhttp

// admin_dev_login_test.go — 一键登录（/admin/dev-login）回跳目标 safeRedirect 的判据。
//
// 为什么单独钉这一条：`?to=` 进的是 `Location` 头（带凭据的入口 + 开放重定向 = 钓鱼跳板），
// 而它的判据曾经是 pkg/safepath 之外**第二份实现**，比分包少一条「控制字符」。
// task-62 只读实证（真 Chrome）：`Location: /\t/evil.example.com` 里浏览器在解析 URL 前
// **先剥掉 TAB**，于是地址栏等价于 `//evil.example.com` —— 协议相对跳转，origin 会被换掉。
// 本文件钉住修复后的行为，并把「旧行为里刻意保留的部分」也写成断言（改了要有人看见）。
//
// 两类断言**分开**记：
//  1. `safeRedirect` 纯函数表（含正向的兼容行为：内部 `//`、超过 512 字节今天仍放行）；
//  2. **真实出口**：gin + httptest 打真实 `DevLogin` handler，看 302 的 Location 实际值。
//     只测字符串归一化替代不了这一条 —— 出口还牵着会话/csrf/环回三道锁，判据挂在出口上。

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

	"go_wp/internal/module/admin/contract"
	"go_wp/internal/module/admin/dto"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/logger"
	"go_wp/pkg/safepath"
)

// devLoginAdminStub 只实现 DevLogin：嵌入接口让 admin 服务其余方法留在原地（本测试不触达它们）。
type devLoginAdminStub struct {
	admincontract.AdminService
	sessionID string
}

func (s devLoginAdminStub) DevLogin(context.Context, string) (*admindto.AdminLoginResp, error) {
	return &admindto.AdminLoginResp{UserID: 1, Username: "tester", SessionID: s.sessionID}, nil
}

// lastPathChars 把不可打印字符换成可见记号，失败信息里能直接看出是哪一个控制字符。
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

// TestSafeRedirect 纯函数表：三条跳站样本（TAB / LF / TAB+反斜杠）+ CRLF/普通路径 +
// 「既有兼容行为」（内部 `//`、513 字节今天仍原样放行）。
func TestSafeRedirect(t *testing.T) {
	long := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes) // 513 字节

	cases := []struct {
		name string
		in   string
		want string
		why  string
	}{
		// —— 正向：站内路径原样放行 ——
		{"根路径", "/", "/", "站内"},
		{"普通后台路径", "/admin/pages", "/admin/pages", "站内"},
		{"带筛选 query", "/admin/coupons?status=1&page=2", "/admin/coupons?status=1&page=2", "站内"},
		{"带片段", "/admin/pages#form", "/admin/pages#form", "站内"},

		// —— 跳站样本（task-62 真 Chrome 复现：剥掉 TAB 后等价 `//主机`）——
		{"TAB 夹在双斜杠之间", "/\t/evil.example.com", "/admin", "控制字符"},
		{"LF 夹在双斜杠之间", "/\n/evil.example.com", "/admin", "控制字符"},
		{"TAB + 反斜杠", "/\t\\evil.example.com", "/admin", "控制字符"},
		{"CR 夹在双斜杠之间", "/\r/evil.example.com", "/admin", "控制字符"},

		// —— 其它控制字符：C0 与 DEL ——
		{"垂直制表", "/\x0b/admin", "/admin", "控制字符"},
		{"中段 NUL", "/admin\x00/pages", "/admin", "控制字符"},
		{"DEL", "/admin\x7fpages", "/admin", "控制字符"},

		// —— CR/LF 拆 Location 头（net/http 会抹成空格，但这类值本就不合法）——
		{"CRLF 注入尝试", "/a\r\nLocation: https://evil.example.com", "/admin", "控制字符"},

		// —— 旧规则保留：前缀 `//` / `/\` / 不以 `/` 开头 / 空白 ——
		{"协议相对前缀", "//evil.example.com/x", "/admin", "前缀//"},
		{"前缀反斜杠（UNC 形态）", `/\\evil.example.com`, "/admin", "前缀/\\"},
		{"无前导斜杠", "admin/roles", "/admin", "非站内"},
		{"绝对 URL", "https://evil.example.com/x", "/admin", "非站内"},
		{"空串", "", "/admin", "非站内"},
		{"仅空白", "   ", "/admin", "非站内"},
		{"前后空白被 trim", " /admin ", "/admin", "TrimSpace 后合法"},

		// —— 既有兼容行为：这次**不动**（收紧会在另一批统一判据时评估）——
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
			// 不变量：出口值要么是兜底 /admin，要么是站内路径且**绝不含控制字符**。
			if safepath.HasControlRune(got) {
				t.Errorf("出口 %q 仍含控制字符（%s）", lastPathChars(got), tc.why)
			}
			if got != "/admin" && !strings.HasPrefix(got, "/") {
				t.Errorf("出口 %q 不是站内绝对路径", lastPathChars(got))
			}
		})
	}
}

// withDevLoginInfra 起 miniredis + 会话/缓存/日志：DevLogin 出口要写 cookie session 与轮换 CSRF。
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

// TestDevLoginRedirectLocationIsControlFree 真实出口：gin + httptest 走真实 DevLogin handler，
// 断言 302 的 Location **实际值**，并确认里面没有控制字符。
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
		// 既有兼容（注意出口与纯函数不同）：safeRedirect 原样返回 `/a//b`，但 `http.Redirect`
		// 对能解析成相对 URL 的值会做路径归一，真正写进 Location 的是 `/a/b` ——
		// 落点仍在站内，这正是本次**不动**子串 `//` 判据的理由（改了要有人看见，故钉住实测值）。
		{"内部双斜杠（既有兼容）", "/a//b", "/a/b", nil},
		{"513 字节（既有兼容）", "/" + strings.Repeat("a", safepath.MaxReturnPathBytes), "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/dev-login?to="+url.QueryEscape(tc.to), nil)
			req.RemoteAddr = "127.0.0.1:23456" // 锁 2：只认环回
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("期望 302，实得 %d（body=%q）", rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			want := tc.want
			if want == "" {
				want = "/" + strings.Repeat("a", safepath.MaxReturnPathBytes) // 超长用例期望原样
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

// TestDevLoginRejectsNonLoopback 锁 2 未被本次改动碰掉：非环回请求一律 403 且不带 Location。
func TestDevLoginRejectsNonLoopback(t *testing.T) {
	withDevLoginInfra(t)
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handle := &Handle{admin: devLoginAdminStub{sessionID: "dev-login-test-session"}}
	engine.GET("/admin/dev-login", handle.DevLogin)

	req := httptest.NewRequest(http.MethodGet, "/admin/dev-login?to=/admin", nil)
	req.RemoteAddr = "203.0.113.7:51234" // 非环回
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("非环回请求期望 403，实得 %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("非环回请求不应带 Location，实得 %q", loc)
	}
}
