package feature

import (
	"net/http"
	"testing"

	"go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	projecthttp "go_wp/internal/module/project/inbound/http"
	"go_wp/internal/permission"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const plainAdminUsername = "feature_plain_admin"

func newAuthFeatureEngine(t *testing.T) (*gin.Engine, *support.AdminSession) {
	t.Helper()

	env, err := support.AcquireTestEnv(t)
	if err != nil {
		t.Skipf("跳过：%v", err)
	}

	db, err := support.NewPGTestDBAt(t, env.PG)
	if err != nil {

		t.Fatalf("创建隔离 PG schema 失败: %v", err)
	}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {

			if err := migrations.Run(db); err != nil {
				t.Fatalf("执行结构迁移失败: %v", err)
			}

			if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
				t.Fatalf("准备测试管理员失败: %v", err)
			}

			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("执行业务权限 seed 失败: %v", err)
			}

			if err := pkgcasbin.InitCasbin(db); err != nil {
				t.Fatalf("初始化 Casbin 失败: %v", err)
			}
			t.Cleanup(func() {
				if err := pkgcasbin.Close(); err != nil {
					t.Errorf("关闭 Casbin 失败: %v", err)
				}

				_ = captcha.Close()
			})

			if err := db.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
				VALUES ('p', '1', '/api/ping', 'GET', 'test:ping'),
				       ('p', '1', '/api/ping', 'POST', 'test:ping')
				ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`).Error; err != nil {
				t.Fatalf("写入测试策略失败: %v", err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
			if err != nil {
				t.Fatalf("生成密码哈希失败: %v", err)
			}
			if err := db.Exec(`DELETE FROM sys_admin WHERE username = ?`, plainAdminUsername).Error; err != nil {
				t.Fatalf("清理非超管测试账号失败: %v", err)
			}
			if err := db.Exec(`INSERT INTO sys_admin (username, password, status, is_admin)
				VALUES (?, ?, 1, 0)`, plainAdminUsername, string(hash)).Error; err != nil {
				t.Fatalf("写入非超管测试账号失败: %v", err)
			}

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)

			authorizedGroup := api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware())
			authorizedAPI := permission.NewRouteGroup(authorizedGroup)

			projecthttp.SetupProjectRoutes(authorizedAPI, db, nil)

			authorizedGroup.GET("/ping", func(c *gin.Context) {
				response.Success(c, gin.H{"pong": true})
			})

			authorizedGroup.POST("/ping", func(c *gin.Context) {
				response.Success(c, gin.H{"pong": true})
			})
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	if err := support.SetupRedisForTestAt(t, env.RedisAddr); err != nil {
		cleanup()

		t.Fatalf("初始化测试 Redis 失败: %v", err)
	}

	sess, err := support.LoginAdminSession(t, engine, support.TestAdminUsername, support.TestAdminPassword)
	if err != nil {
		t.Skipf("跳过：登录链路不可用（%v）", err)
	}
	return engine, sess
}

func TestCaptchaRouteExemptedFromAuth(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/captcha",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("验证码路由应豁免认证: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var data struct {
		CaptchaID string `json:"captcha_id"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if data.CaptchaID == "" {
		t.Fatalf("验证码路由应返回 captcha_id")
	}
}

func TestApiBusinessRoutesRejectAnonymousJSONRequest(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 JSON 请求应返回 401: got=%d want=%d", recorder.Code, http.StatusUnauthorized)
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("响应 code 不正确: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}
}

func TestApiBusinessRoutesPageRequestsStay401JSON(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{name: "浏览器页面导航", headers: map[string]string{"Accept": "text/html,application/xhtml+xml"}},
		{name: "HTMX 请求", headers: map[string]string{"HX-Request": "true"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder, err := support.SendRequest(engine, support.RequestOptions{
				Method:  http.MethodGet,
				Path:    "/api/ping",
				Headers: tc.headers,
			})
			if err != nil {
				t.Fatalf("发送请求失败: %v", err)
			}
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s 应 401（不跳转）: got=%d want=%d body=%s",
					tc.name, recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
			if loc := recorder.Header().Get("Location"); loc != "" {
				t.Fatalf("%s 不应下发 Location（拒绝响应不改写成跳转）: got=%q", tc.name, loc)
			}
			resp, perr := support.ParseStandardResponse(recorder)
			if perr != nil {
				t.Fatalf("%s 拒绝对应是 JSON 标准响应，解析失败: %v", tc.name, perr)
			}
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("%s 信封 code 应为 401，实际 %d", tc.name, resp.Code)
			}
		})
	}
}

func TestApiBusinessRoutesAllowLoggedInAdmin(t *testing.T) {
	engine, sess := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Cookie": sess.Cookie,
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("已登录管理员应可访问业务 API: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var data struct {
		Pong bool `json:"pong"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if !data.Pong {
		t.Fatalf("handler 应正常执行并返回 pong=true")
	}
}

func TestApiBusinessRoutesRejectPostWithoutCSRF(t *testing.T) {
	engine, sess := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Cookie": sess.Cookie,
		},
		Body: gin.H{},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("登录后无 CSRF token 的 POST 应被 403 拦截: got=%d want=%d body=%s",
			recorder.Code, http.StatusForbidden, recorder.Body.String())
	}

	resp, err := support.ParseStandardResponse(recorder)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != http.StatusForbidden {
		t.Fatalf("响应 code 不正确: got=%d want=%d", resp.Code, http.StatusForbidden)
	}
}

func TestApiBusinessRoutesAllowPostWithCSRF(t *testing.T) {
	engine, sess := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Cookie":       sess.Cookie,
			"X-CSRF-Token": sess.CSRFToken,
		},
		Body: gin.H{},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("携带 CSRF token 的 POST 应放行: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var data struct {
		Pong bool `json:"pong"`
	}
	if err := support.DecodeResponseData(recorder, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if !data.Pong {
		t.Fatalf("handler 应正常执行并返回 pong=true")
	}
}

func TestBusinessAPISuperAdminAllowedAfterSeed(t *testing.T) {
	engine, sess := newAuthFeatureEngine(t)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/project/list",
		Headers: map[string]string{
			"Cookie": sess.Cookie,
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("超管应可访问已授权业务 API: got=%d want=%d body=%s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if code, ok := pkgcasbin.GetCodeByURL("/api/page/list", "GET"); !ok || code != "page:list" {
		t.Fatalf("GetCodeByURL 应命中页面列表权限点: code=%q ok=%v", code, ok)
	}
	if code, ok := pkgcasbin.GetCodeByURL("/api/page/publish", "POST"); !ok || code != "page:publish" {
		t.Fatalf("GetCodeByURL 应命中发布权限点: code=%q ok=%v", code, ok)
	}
}

func TestBusinessAPINonSuperAdminForbiddenWithoutPolicy(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	sess, err := support.LoginAdminSession(t, engine, plainAdminUsername, support.TestAdminPassword)
	if err != nil {
		t.Skipf("跳过：非超管登录链路不可用（%v）", err)
	}

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/project/list",
		Headers: map[string]string{
			"Cookie": sess.Cookie,
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("非超管无策略应 403 拒绝: got=%d want=%d body=%s",
			recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}
