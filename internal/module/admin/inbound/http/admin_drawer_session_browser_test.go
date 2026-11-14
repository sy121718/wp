package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	pageview "go_wp/pkg/view"

	"github.com/spf13/viper"
)

// 共享抽屉的真实认证链：真会话（Redis 会话存储）+ 真 CSRF（同一个 EnsureCSRFToken
// 签发、同一个 CSRFMiddleware 校验），业务服务仍是内存夹具。
//
// 与 admin_drawer_browser_test.go 的分工：
//
//   - 那一份用固定 token + 固定 user_id 桩，证明抽屉的三视口 × 四输入交互；
//   - 这一份不重复那套交互，只替换**凭证从哪来、服务端怎么校验**：浏览器从页面
//     拿到会话 cookie 与 csrf meta 再提交；缺 cookie 必须 401、有 cookie 缺 token
//     必须 403、两者齐全才落进 Controller。
//
// 业务 service 仍是夹具（内存里的菜单行）：行内容不是本用例的验收对象。
func TestAdminDrawerRealSessionAndCSRF(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 验证抽屉的真实会话 + 真实 CSRF")
	}

	// 不能引 public/test/support：support → routers → 本包 会成环。
	// 这里按 support 的同一套装配自己起 Redis（本仓库唯一用的 DB 由目录名隔离，
	// 包内测试固定用 10 号库，与 support 的分配表不重叠）。
	redisAddr := os.Getenv("GOWP_TEST_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	cfg := viper.New()
	cfg.Set("redis.addrs", []string{redisAddr})
	cfg.Set("redis.db", 10)
	cfg.Set("auth.session_secret", "drawer-session-browser-test-secret-32")
	if err := cache.Init(cfg); err != nil {
		t.Skipf("真实会话需要 Redis（%s 不可用）: %v", redisAddr, err)
	}
	if err := auth.Init(cfg); err != nil {
		_ = cache.Close()
		t.Skipf("初始化 auth 需要 Redis（%s 不可用）: %v", redisAddr, err)
	}
	t.Cleanup(func() { _ = auth.Close(); _ = cache.Close() })

	// 真实会话数据：会话索引 + 用户会话都进 Redis，SessionAuthMiddleware 才认。
	ctx := context.Background()
	sessionID, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("生成会话 id 失败: %v", err)
	}
	const adminUserID uint64 = 123
	if err := auth.SaveUserSession(ctx, &auth.UserSession{
		ID: adminUserID, SessionID: sessionID, Username: "drawer-session-admin", IsAdmin: 1, Status: 1,
	}, time.Hour); err != nil {
		t.Fatalf("写入用户会话失败: %v", err)
	}

	menus := &browserMenuService{drawerMenuService: drawerMenuService{
		row: &admindto.MenuDetailResp{
			ID: 42, Title: "原菜单", ParentID: 7, Type: 2, Path: "/admin/demo",
			Status: 0, PermissionCodes: []string{"menu:list"},
		},
		parents: []admindto.MenuParentChoice{
			{ID: 7, Title: "目录", Type: 1},
			{ID: 42, Title: "原菜单", Type: 2, Disabled: true},
		},
	}}
	// 抽屉片段要读权限候选（buildPermChoices）：少了它模板渲染会 panic，
	// 浏览器拿到的是空响应（net::ERR_EMPTY_RESPONSE）而不是 500。
	perms := &browserPermService{drawerPermService: drawerPermService{
		row: &admindto.PermDetailResp{ID: 42, PermissionCode: "permission:demo", PermissionName: "原权限",
			Module: "admin", APIPath: "/api/demo", APIMethod: "POST", Status: 0},
	}}
	h := &AdminPagesHandle{menus: menus, perms: perms}

	engine := drawerEngine()
	engine.GET("/favicon.ico", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.GET("/static/js/*name", func(c *gin.Context) {
		source, err := templates.StaticJS(c.Param("name")[1:])
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(source))
	})
	engine.GET("/static/css/*name", func(c *gin.Context) {
		body, err := templates.AssetFile("css/" + c.Param("name")[1:])
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", body)
	})

	// 登录：真实 SaveCookieSession 写 cookie，会话本身已在 Redis 里。
	// 响应必须是一张可渲染的页面：204/空体会让浏览器中止导航（ERR_ABORTED）。
	engine.GET("/login", func(c *gin.Context) {
		if err := auth.SaveCookieSession(c, &auth.CookieSession{SessionID: sessionID}, false); err != nil {
			t.Errorf("写入会话 cookie 失败: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(`<!doctype html><meta charset="utf-8"><title>ok</title><a href="/">go</a>`))
	})

	// 页面：真实 EnsureCSRFToken 签发（写进会话），渲染进 meta 供写基座读取。
	engine.GET("/", func(c *gin.Context) {
		token, terr := builtin.EnsureCSRFToken(c)
		if terr != nil {
			t.Errorf("签发 CSRF token 失败: %v", terr)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(drawerSessionPage(token)))
	})

	guarded := engine.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware())
	guarded.GET("/admin/menus/edit", pageview.HTMLFragment, h.MenusEditFragment)
	guarded.POST("/admin/menus/update", pageview.HTMLFragment, h.MenusUpdate)

	server := httptest.NewServer(engine)
	defer server.Close()

	// 拒绝面先在进程内验（不需要浏览器）：两个中间件都是生产实现。
	t.Run("缺会话 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/menus/update", nil)
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("无会话写入应 401，实际 %d：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("有会话缺 CSRF 403", func(t *testing.T) {
		loginRec := httptest.NewRecorder()
		engine.ServeHTTP(loginRec, httptest.NewRequest(http.MethodGet, "/login", nil))
		cookies := loginRec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("登录没有写入会话 cookie")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/menus/update", nil)
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("有会话但缺 CSRF 应 403，实际 %d：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("浏览器真实会话 + 真实 CSRF", func(t *testing.T) {
		cmd := exec.Command("node", "--test", "testdata/html-ui/admin-drawer-session.cjs")
		cmd.Env = append(os.Environ(),
			"GOWP_HTML_UI_BASE_URL="+server.URL,
			"GOWP_PUPPETEER_MODULE="+puppeteerModule(),
			"GOWP_CHROME="+chromePath(),
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("抽屉真实会话 + 真实 CSRF 验收失败：%v\n%s", err, output)
		}
		t.Logf("%s", output)

		menus.mu.Lock()
		defer menus.mu.Unlock()
		if menus.updated == nil || menus.updated.Title != "菜单 375" {
			t.Fatalf("菜单提交事实错误：%+v", menus.updated)
		}
	})
}

// drawerSessionPage 是抽屉宿主的同形外壳：脚本与控件都来自生产源，
// 只有 csrf meta 由服务端按真实会话签发。
func drawerSessionPage(token string) string {
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="csrf-token" content="` + token + `">
    <link rel="stylesheet" href="/static/css/ui.css">
    <title>抽屉真实会话验收</title>
</head>
<body>
    <button id="menu" type="button" data-drawer-open hx-get="/admin/menus/edit?id=42" hx-target="#admin-drawer-body" hx-swap="innerHTML" data-drawer-title="编辑菜单">编辑菜单</button>
    <div class="drawer-mask" data-drawer-mask hidden></div>
    <aside class="drawer" data-drawer data-write-host hidden>
        <header><span data-drawer-title></span><button type="button" data-drawer-close>关闭</button></header>
        <div data-write-status role="alert" hidden></div>
        <div id="admin-drawer-body" class="drawer-body" data-drawer-body></div>
    </aside>
    <script src="/static/js/ui/htmx.min.js"></script>
    <script src="/static/js/ui/write-base.js"></script>
    <script src="/static/js/ui/htmx-bridge.js"></script>
    <script src="/static/js/ui/_util.js"></script>
    <script src="/static/js/ui/select.js"></script>
    <script src="/static/js/ui/drawer.js"></script>
    <script src="/static/js/ui/iconfield.js"></script>
    <script src="/static/js/ui/perm-picker.js"></script>
    <script src="/static/js/ui/index.js"></script>
</body>
</html>`
}

func puppeteerModule() string {
	if v := os.Getenv("GOWP_PUPPETEER_MODULE"); v != "" {
		return v
	}
	return "/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core"
}

func chromePath() string {
	if v := os.Getenv("GOWP_CHROME"); v != "" {
		return v
	}
	return "/usr/bin/google-chrome"
}
