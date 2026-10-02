package feature

// api_auth_test.go — 业务 API 统一认证（SessionAuthMiddleware）+ 鉴权（CasbinMiddleware）的接口链路测试。
//
// 覆盖点：
//   - GET /api/captcha 豁免认证，匿名可达
//   - 未登录访问业务 API（JSON 请求）→ 401 JSON
//   - 未登录的页面类请求（Accept: text/html 的 GET / HTMX）→ 302 登录页
//   - 登录成功（验证码 + PG + Redis 真实链路）后带 Cookie 访问业务 API → 200
//   - 业务权限 seed 后：超管（is_admin=1）可访问已授权业务 API；urlCodeMap 收录业务路径
//   - 非超管（is_admin=0，无任何策略）访问业务 API → 403 默认拒绝
//
// 依赖 PostgreSQL 与 Redis：本地可用优先本地，否则自动回退 testcontainers
// 容器（support.AcquireTestEnv），两级均不可用时整体 Skip（不 Fail）。

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

// plainAdminUsername 非超管测试账号（is_admin=0，无任何 Casbin 策略）。
const plainAdminUsername = "feature_plain_admin"

// newAuthFeatureEngine 组装带统一认证 + CSRF + Casbin 鉴权的业务 API 测试引擎，
// 返回引擎与已登录超管会话（Cookie + CSRF Token）。
// PG / Redis 任一不可用时 t.Skip；其余环节失败视为测试自身问题，直接 Fatal。
func newAuthFeatureEngine(t *testing.T) (*gin.Engine, *support.AdminSession) {
	t.Helper()

	// 三级回退获取 PG + Redis 端点：本地优先 → Docker 容器 → Skip 兜底。
	env, err := support.AcquireTestEnv(t)
	if err != nil {
		t.Skipf("跳过：%v", err)
	}

	db, err := support.NewPGTestDBAt(t, env.PG)
	if err != nil {
		// 端点已经 AcquireTestEnv 探测就绪，此处失败视为测试自身问题。
		t.Fatalf("创建隔离 PG schema 失败: %v", err)
	}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		UseDefaultRoute: false,
		InitComponents:  false,
		RouteRegistrar: func(engine *gin.Engine) {
			// 与 routes.go 相同的认证+鉴权装配结构：captcha 与 admin 豁免，
			// 业务 API 统一挂 SessionAuthMiddleware + CSRFMiddleware + CasbinMiddleware。
			// 1) 结构迁移：建全量表（sys_admin/sys_permission/sys_menus/sys_casbin_rule 等）
			if err := migrations.Run(db); err != nil {
				t.Fatalf("执行结构迁移失败: %v", err)
			}
			// 2) 准备测试超管（is_admin=1，ID=1），必须在业务权限 seed 之前
			if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
				t.Fatalf("准备测试管理员失败: %v", err)
			}
			// 3) 业务权限 seed：权限点 + 菜单 + 超管全量策略
			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("执行业务权限 seed 失败: %v", err)
			}
			// 4) 初始化 Casbin（加载 seed 策略到内存并重建 urlCodeMap）
			if err := pkgcasbin.InitCasbin(db); err != nil {
				t.Fatalf("初始化 Casbin 失败: %v", err)
			}
			t.Cleanup(func() {
				if err := pkgcasbin.Close(); err != nil {
					t.Errorf("关闭 Casbin 失败: %v", err)
				}
				// 停止 captcha.Get() 自动初始化产生的过期清理协程（goleak 泄漏检测要求）。
				_ = captcha.Close()
			})
			// 5) 测试专用路径 /api/ping（GET+POST）：直接写入超管策略，
			//    避免 ReplaceUserPermissions 清空 seed 授权
			if err := db.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
				VALUES ('p', '1', '/api/ping', 'GET', 'test:ping'),
				       ('p', '1', '/api/ping', 'POST', 'test:ping')
				ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`).Error; err != nil {
				t.Fatalf("写入测试策略失败: %v", err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}
			// 6) 非超管测试账号（is_admin=0，无任何策略）。
			//
			// **不要再用固定 id=2**（2026-09-19 修）：SeedTestAdmin 会把 sys_admin 的 id 序列
			// setval 到 MAX(id)=1，紧接着 RunSeeds 里 030a 的默认超管 seed（不带 id、走 nextval）
			// 恰好拿到 2 —— 固定 id=2 与它撞上，而 ON CONFLICT (id) DO NOTHING 是**静默**跳过。
			// 后果：账号根本没建出来 → 登录返回 400 ErrInvalidPassword → 本用例走
			// t.Skipf("跳过：非超管登录链路不可用")，而它与「环境不可用」长得一模一样，
			// 于是「越权请求必须 403」这条断言**从未真正执行过**（`go test` 只显示 SKIP，全绿）。
			// 改为「按 username 先删后插、id 交给序列」：与 seed 互不干扰，且可重复运行。
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

			// 与 routes.go 同款：三层链外面包一层声明式权限路由组（路由注册时声明权限点）。
			authorizedGroup := api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware())
			authorizedAPI := permission.NewRouteGroup(authorizedGroup)
			// 真实业务模块装配（与 routes.go 相同的中间件链），验证 seed 策略在真实业务链路上生效
			projecthttp.SetupProjectRoutes(authorizedAPI, db, nil)
			// /ping 是测试专用探针（策略在上面显式写入 sys_casbin_rule），不参与权限点声明：
			// 直接挂底层组，避免为一条测试路径造一条权限点。
			authorizedGroup.GET("/ping", func(c *gin.Context) {
				response.Success(c, gin.H{"pong": true})
			})
			// 业务写接口样例：POST 需通过 CSRF 校验（与 /api/page/draft/save 等同一中间件链）。
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
		// 端点已经 AcquireTestEnv 探测就绪，此处失败视为测试自身问题。
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

func TestApiBusinessRoutesRedirectPageRequestsToLogin(t *testing.T) {
	engine, _ := newAuthFeatureEngine(t)

	// 浏览器页面导航：GET + Accept: text/html → 302 登录页。
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusFound {
		t.Fatalf("页面类 GET 请求应 302: got=%d want=%d", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/login" {
		t.Fatalf("重定向地址不正确: got=%s want=%s", got, "/admin/login")
	}

	// HTMX 请求：HX-Request: true → 302 登录页。
	recorder, err = support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/api/ping",
		Headers: map[string]string{
			"HX-Request": "true",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	if recorder.Code != http.StatusFound {
		t.Fatalf("HTMX 请求应 302: got=%d want=%d", recorder.Code, http.StatusFound)
	}
	if got := recorder.Header().Get("Location"); got != "/admin/login" {
		t.Fatalf("HTMX 重定向地址不正确: got=%s want=%s", got, "/admin/login")
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

// TestApiBusinessRoutesRejectPostWithoutCSRF 业务 API 写操作无 CSRF token → 403：
// authorizedAPI 组挂载 CSRFMiddleware 后，POST 必须携带 X-CSRF-Token（或表单字段）。
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

// TestApiBusinessRoutesAllowPostWithCSRF 业务 API 写操作携带登录下发的 token → 放行。
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

// TestBusinessAPISuperAdminAllowedAfterSeed 验证业务权限 seed 后：
//   - 超管（is_admin=1，seed 已赋全量策略）可访问真实业务 API（/api/project/list）
//   - urlCodeMap 已收录业务路径 → code（GetCodeByURL）
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

	// urlCodeMap 应收录业务路径（rebuildURLCodeMap 遍历 seed 策略自动构建）
	if code, ok := pkgcasbin.GetCodeByURL("/api/page/list", "GET"); !ok || code != "page:list" {
		t.Fatalf("GetCodeByURL 应命中页面列表权限点: code=%q ok=%v", code, ok)
	}
	if code, ok := pkgcasbin.GetCodeByURL("/api/page/publish", "POST"); !ok || code != "page:publish" {
		t.Fatalf("GetCodeByURL 应命中发布权限点: code=%q ok=%v", code, ok)
	}
}

// TestBusinessAPINonSuperAdminForbiddenWithoutPolicy 验证非超管（is_admin=0、无任何策略）
// 访问业务 API 默认被 Casbin 拒绝（403）。
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
